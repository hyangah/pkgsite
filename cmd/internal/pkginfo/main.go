// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command pkginfo queries the pkg.go.dev API for information about
// Go packages and modules.
//
// Usage:
//
//	pkginfo [flags] <package>[@version]       package information
//	pkginfo module [flags] <module>[@version]  module information
//	pkginfo search [flags] <query>             search for packages
//
// See doc/pkginfo.md for the full design document.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return dispatch(args, commands(), stdout, stderr)
}

func commands() []*command {
	var pf packageFlags
	pkgFS := flag.NewFlagSet("pkginfo", flag.ContinueOnError)
	pf.register(pkgFS)

	var mf moduleFlags
	modFS := flag.NewFlagSet("pkginfo module", flag.ContinueOnError)
	mf.register(modFS)

	var sf searchFlags
	searchFS := flag.NewFlagSet("pkginfo search", flag.ContinueOnError)
	sf.register(searchFS)

	return []*command{
		{
			name:    "",
			args:    "<package>[@version]",
			summary: "package information",
			flags:   pkgFS,
			run:     func(fs *flag.FlagSet, stdout, stderr io.Writer) int { return runPackage(fs, &pf, stdout, stderr) },
		},
		{
			name:    "module",
			args:    "<module>[@version]",
			summary: "module information",
			flags:   modFS,
			run:     func(fs *flag.FlagSet, stdout, stderr io.Writer) int { return runModule(fs, &mf, stdout, stderr) },
		},
		{
			name:    "search",
			args:    "<query>",
			summary: "search for packages",
			flags:   searchFS,
			run:     func(fs *flag.FlagSet, stdout, stderr io.Writer) int { return runSearch(fs, &sf, stdout, stderr) },
		},
	}
}

// splitPathVersion splits "path@version" into its components.
// If there is no @, version is empty.
func splitPathVersion(s string) (path, version string) {
	if i := strings.LastIndex(s, "@"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// handleErr writes an error message. In JSON mode, the error is written
// to stdout as a JSON object so callers can parse it. In text mode, it
// goes to stderr.
func handleErr(stdout, stderr io.Writer, err error, jsonMode bool) int {
	if jsonMode {
		aerr, ok := err.(*apiError)
		if !ok {
			aerr = &apiError{Code: 1, Message: err.Error()}
		}
		writeJSON(stdout, stderr, aerr)
		return 1
	}
	fmt.Fprintln(stderr, err)
	return 1
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
