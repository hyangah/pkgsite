// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fetch

import (
	"archive/zip"
	"context"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/derrors"
	"golang.org/x/pkgsite/internal/fuzzy"
	"golang.org/x/pkgsite/internal/log"
	"golang.org/x/pkgsite/internal/version"
)

// modCacheSearchEntry describes one package found in the module cache.
type modCacheSearchEntry struct {
	modulePath  string
	version     string // semver, e.g. "v0.30.0"
	packagePath string
	packageName string
	synopsis    string
}

// Search implements fuzzy search over the packages present in the module
// cache. It matches against package import paths.
//
// The index is built lazily on the first call and reused for the lifetime
// of the getter. Modules downloaded after the index is built are not
// reflected until the server is restarted.
func (g *modCacheModuleGetter) Search(ctx context.Context, query string, limit int) ([]*internal.SearchResult, error) {
	entries, err := g.searchIndex(ctx)
	if err != nil {
		return nil, err
	}
	matcher := fuzzy.NewSymbolMatcher(query)

	type scored struct {
		e     *modCacheSearchEntry
		score float64
	}
	var scores []scored
	for i := range entries {
		_, s := matcher.Match([]string{entries[i].packagePath})
		if s <= 0 {
			continue
		}
		scores = append(scores, scored{&entries[i], s})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	if limit > 0 && len(scores) > limit {
		scores = scores[:limit]
	}
	var results []*internal.SearchResult
	for i, sc := range scores {
		results = append(results, &internal.SearchResult{
			Name:        sc.e.packageName,
			PackagePath: sc.e.packagePath,
			ModulePath:  sc.e.modulePath,
			Version:     sc.e.version,
			Synopsis:    sc.e.synopsis,
			Score:       sc.score,
			Offset:      i,
		})
	}
	return results, nil
}

// searchIndex returns the lazily built index of cached packages.
func (g *modCacheModuleGetter) searchIndex(ctx context.Context) ([]modCacheSearchEntry, error) {
	g.indexOnce.Do(func() {
		g.index, g.indexErr = g.buildSearchIndex(ctx)
	})
	return g.index, g.indexErr
}

// buildSearchIndex scans the module cache and returns a list of searchable
// packages. For each module path, the latest semantic version present in
// the cache is chosen and its packages are enumerated from the zip file.
func (g *modCacheModuleGetter) buildSearchIndex(ctx context.Context) (_ []modCacheSearchEntry, err error) {
	defer derrors.Wrap(&err, "modCacheModuleGetter.buildSearchIndex")
	start := time.Now()

	versionsByModule, err := findCachedModuleVersions(filepath.Join(g.dir, "cache", "download"))
	if err != nil {
		return nil, err
	}

	var entries []modCacheSearchEntry
	for mod, versions := range versionsByModule {
		ver := version.LatestOf(versions)
		if ver == "" {
			continue
		}
		zipPath, err := g.escapedPath(mod, ver, "zip")
		if err != nil {
			continue
		}
		pkgs, err := packagesInZip(zipPath, mod, ver)
		if err != nil {
			log.Debugf(ctx, "modCacheModuleGetter: skipping %s@%s: %v", mod, ver, err)
			continue
		}
		entries = append(entries, pkgs...)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].packagePath < entries[j].packagePath
	})
	log.Infof(ctx, "modCacheModuleGetter: indexed %d packages from %d modules in %v",
		len(entries), len(versionsByModule), time.Since(start))
	return entries, nil
}

// findCachedModuleVersions walks downloadDir (typically
// $GOMODCACHE/cache/download) and returns, for each module path, the set of
// versions whose .zip file is present.
func findCachedModuleVersions(downloadDir string) (map[string][]string, error) {
	result := make(map[string][]string)
	err := filepath.WalkDir(downloadDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Ignore per-entry errors; a partially readable cache should
			// not abort indexing.
			return nil
		}
		// Skip the sumdb subtree and other non-module directories.
		if d.IsDir() && d.Name() == "sumdb" {
			return fs.SkipDir
		}
		if !d.IsDir() || d.Name() != "@v" {
			return nil
		}
		rel, err := filepath.Rel(downloadDir, filepath.Dir(p))
		if err != nil {
			return fs.SkipDir
		}
		modPath, err := module.UnescapePath(filepath.ToSlash(rel))
		if err != nil {
			return fs.SkipDir
		}
		dirents, err := os.ReadDir(p)
		if err != nil {
			return fs.SkipDir
		}
		for _, e := range dirents {
			name := e.Name()
			if !strings.HasSuffix(name, ".zip") {
				continue
			}
			ver, err := module.UnescapeVersion(strings.TrimSuffix(name, ".zip"))
			if err != nil {
				continue
			}
			result[modPath] = append(result[modPath], ver)
		}
		return fs.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// packagesInZip opens modPath@ver.zip and returns one entry per
// non-test, non-internal package directory it contains. Entries with
// package name "main" are skipped.
func packagesInZip(zipPath, modPath, ver string) (_ []modCacheSearchEntry, err error) {
	defer derrors.Wrap(&err, "packagesInZip(%q)", zipPath)

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	prefix := modPath + "@" + ver + "/"
	goFilesByDir := make(map[string][]*zip.File)
	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, ".go") || strings.HasSuffix(f.Name, "_test.go") {
			continue
		}
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		dir, _ := path.Split(rel)
		dir = strings.TrimSuffix(dir, "/")
		if shouldSkipPackageDir(dir) {
			continue
		}
		goFilesByDir[dir] = append(goFilesByDir[dir], f)
	}

	var entries []modCacheSearchEntry
	for dir, files := range goFilesByDir {
		name, synopsis, ok := readPackageDoc(files)
		if !ok {
			continue
		}
		pkgPath := modPath
		if dir != "" {
			pkgPath = modPath + "/" + dir
		}
		entries = append(entries, modCacheSearchEntry{
			modulePath:  modPath,
			version:     ver,
			packagePath: pkgPath,
			packageName: name,
			synopsis:    synopsis,
		})
	}
	return entries, nil
}

// shouldSkipPackageDir reports whether a package directory within a module
// should be excluded from the search index.
func shouldSkipPackageDir(dir string) bool {
	if dir == "" {
		return false
	}
	for part := range strings.SplitSeq(dir, "/") {
		switch part {
		case "testdata", "vendor", "internal":
			return true
		}
	}
	return false
}

// readPackageDoc parses files to determine the package's name and synopsis.
// It prefers doc.go and returns as soon as it has found a non-empty synopsis.
func readPackageDoc(files []*zip.File) (name, synopsis string, ok bool) {
	sort.Slice(files, func(i, j int) bool {
		iDoc := path.Base(files[i].Name) == "doc.go"
		jDoc := path.Base(files[j].Name) == "doc.go"
		if iDoc != jDoc {
			return iDoc
		}
		return files[i].Name < files[j].Name
	})
	for _, f := range files {
		pkgName, syn, err := readSinglePackageDoc(f)
		if err != nil {
			continue
		}
		if pkgName == "" || pkgName == "main" {
			continue
		}
		name = pkgName
		if syn != "" {
			return name, syn, true
		}
	}
	if name != "" {
		return name, "", true
	}
	return "", "", false
}

func readSinglePackageDoc(f *zip.File) (name, synopsis string, err error) {
	rc, err := f.Open()
	if err != nil {
		return "", "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", "", err
	}
	tr, err := parser.ParseFile(token.NewFileSet(), f.Name, data,
		parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return "", "", err
	}
	name = tr.Name.Name
	if tr.Doc != nil {
		synopsis = doc.Synopsis(tr.Doc.Text()) //nolint:staticcheck // SA1019: we don't have a doc.Package here, and Package.Synopsis' link handling would hurt search.
	}
	return name, synopsis, nil
}
