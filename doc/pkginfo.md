# pkginfo: a CLI for querying pkg.go.dev

## Motivation

The pkg.go.dev website exposes a set of v1 JSON API endpoints that serve
package metadata, module information, search results, vulnerability reports,
and more. These endpoints are available at paths like `/v1/package/`,
`/v1/module/`, `/v1/search/`, etc.

Currently there is no command-line tool that provides access to this
information. Developers who want to check versions, vulnerabilities,
reverse dependencies, or licenses must open a browser, or interact with
the low level REST API. AI coding agents that need authoritative Go
package metadata must access and parse the webpage or understand the
REST API.

`pkginfo` is a lightweight CLI that queries the pkg.go.dev v1 API and
prints the results to the terminal (human-readable text) or stdout
(structured JSON for scripts and AI agents).

### Relationship to existing tools

- **`go doc`** renders documentation for packages available locally (in the
  module cache, workspace, or standard library). It works offline and handles
  unexported symbols. `pkginfo` does not replace `go doc` for reading docs.
- **`pkginfo`** provides information that `go doc` cannot: version listings,
  vulnerability reports, reverse dependencies, license information, search,
  and documentation for packages you haven't downloaded yet.
- **`cmd/pkgsite`** is a local documentation server that opens a browser.
  It uses a `FetchDataSource` that does not support imported-by, full search,
  or version listing. `pkginfo` talks to the production pkg.go.dev API and
  has access to all data.

Rule of thumb: use `go doc` to read docs for code you have locally; use
`pkginfo` for discovery, evaluation, and metadata about the broader ecosystem.

## Command design

`pkginfo` has two entity commands and one action command:

```
pkginfo [flags] <package>[@version]           # package information
pkginfo module [flags] <module>[@version]     # module information
pkginfo search [flags] <query>                # search for packages
```

Everything else is a flag on one of these — there are no subcommands for
versions, vulnerabilities, symbols, etc. Each flag triggers at most one
additional API call.

### Package information

```
pkginfo [flags] <package>[@version]
```

With no flags, prints a concise overview: package path, module path,
version, synopsis, whether it is the latest version, and whether it is
part of the standard library.

Example output:

```
encoding/json (standard library)
  Module:   std
  Version:  go1.24.2 (latest)
  Synopsis: Package json implements encoding and decoding of JSON as
            defined in RFC 7159.
```

```
golang.org/x/text/language@v0.25.0
  Module:   golang.org/x/text
  Version:  v0.25.0 (latest)
  Synopsis: Package language implements BCP 47 language tags and
            related functionality.
```

Flags:

| Flag | Description |
|---|---|
| `--doc[=text\|md\|html]` | Render package documentation. Default format is `text`. |
| `--examples` | Include examples in rendered documentation. Requires `--doc`. |
| `--imports` | List packages imported by this package. |
| `--imported-by` | List packages that import this package (reverse dependencies). |
| `--symbols` | List exported symbols (functions, types, constants, variables). |
| `--licenses` | Show license information. |
| `--module=<path>` | Disambiguate when a package path exists in multiple modules. If omitted and the path is ambiguous, prints the candidates and exits with an error. |
| `--goos=<os>` | Target GOOS for platform-specific symbols and docs. |
| `--goarch=<arch>` | Target GOARCH for platform-specific symbols and docs. |

### Module information

```
pkginfo module [flags] <module>[@version]
```

With no flags, prints: module path, version, whether it is the latest
version, repository URL, whether it has a go.mod, and whether it is
redistributable.

Example output:

```
golang.org/x/text@v0.25.0
  Version:          v0.25.0 (latest)
  Repository:       https://github.com/golang/text
  Has go.mod:       yes
  Redistributable:  yes
```

Flags:

| Flag | Description |
|---|---|
| `--readme` | Print the module's README contents. |
| `--licenses` | List licenses with file paths and contents. |
| `--versions` | List available versions. |
| `--vulns` | List known vulnerabilities. |
| `--packages` | List packages contained in the module. |

### Search

```
pkginfo search [flags] <query>
```

Searches pkg.go.dev and prints matching packages with their module path,
version, and synopsis.

Flags:

| Flag | Description |
|---|---|
| `--symbol=<name>` | Search for a specific symbol name. |

### Common flags

These flags are available on all commands:

| Flag | Description |
|---|---|
| `--json` | Output structured JSON instead of human-readable text. |
| `--limit=N` | Maximum number of results for list outputs (default: 20 for text, 100 for JSON). |
| `--server=URL` | API server URL (default: `https://pkg.go.dev`). Override to use a local pkgsite instance or a private module server. |

## Version syntax

Versions use the `@version` suffix, matching the convention used by
`go get`, `go install`, and pkg.go.dev URLs:

```
pkginfo encoding/json                      # latest
pkginfo encoding/json@go1.22.0             # specific Go version (stdlib)
pkginfo module golang.org/x/text@v0.14.0   # specific module version
pkginfo module golang.org/x/text@latest     # explicit latest (same as omitting)
```

If `@version` is omitted, the latest version is used.

## Module disambiguation

A package path can exist in multiple modules. For example, a package
`github.com/foo/bar/pkg` could belong to module `github.com/foo/bar` or
`github.com/foo/bar/pkg` (if it is its own module).

When the path is ambiguous, `pkginfo` prints an error with candidates:

```
$ pkginfo github.com/foo/bar/pkg
error: ambiguous package path; specify --module:
  --module=github.com/foo/bar
  --module=github.com/foo/bar/pkg
```

Use `--module` to resolve:

```
$ pkginfo github.com/foo/bar/pkg --module=github.com/foo/bar
```

## Pagination

List outputs (versions, packages, symbols, imported-by, search results)
are paginated.

In text mode, `pkginfo` prints up to `--limit` results (default 20)
followed by a summary line:

```
Showing 20 of 347 results. Use --limit=N to see more.
```

In JSON mode (`--json`), the response includes a `nextPageToken` field.
Pass it back with `--token` to fetch the next page:

```
$ pkginfo search --json --limit=10 "json parser"
{"items": [...], "total": 347, "nextPageToken": "10"}

$ pkginfo search --json --limit=10 --token=10 "json parser"
{"items": [...], "total": 347, "nextPageToken": "20"}
```

The `--token` flag is only meaningful in JSON mode and is intended for
scripts and AI agents that paginate programmatically.

## JSON output

With `--json`, the output is structured JSON using the tool's own schema,
which wraps and simplifies the v1 API responses. Field names follow the
same conventions as the API types in `internal/api/types.go`.

For package queries with multiple flags (e.g., `--symbols --imported-by`),
the JSON output combines the results into a single object:

```json
{
  "package": { ... },
  "symbols": { "items": [...], "total": 42 },
  "importedBy": { "items": [...], "total": 128 }
}
```

## AI agent usage

`pkginfo` is designed to be used by AI coding agents (via Claude Code
skills, tool definitions, or direct shell invocation). The `--json` flag
produces structured output suitable for programmatic consumption.

A SKILL.md or tool description should guide agents on when to use each
command:

- **`pkginfo --symbols --json <pkg>`** — before writing code that calls an
  unfamiliar package. Eliminates hallucinated function signatures.
- **`pkginfo --imported-by --json <pkg>`** — before making a breaking
  change. Understand the blast radius.
- **`pkginfo module --vulns --json <mod>`** — during dependency review or
  security audit.
- **`pkginfo search --json <query>`** — when looking for a package to solve
  a problem.
- **`go doc <pkg>`** (not pkginfo) — when the agent needs to read the
  full documentation of a locally available package.

## API mapping

Each command and flag maps to one v1 API call:

| Command / flag | API endpoint |
|---|---|
| `pkginfo <pkg>` | `GET /v1/package/<pkg>` |
| `  --doc` | `GET /v1/package/<pkg>?doc=text` |
| `  --imports` | `GET /v1/package/<pkg>?imports=true` |
| `  --licenses` | `GET /v1/package/<pkg>?licenses=true` |
| `  --symbols` | `GET /v1/symbols/<pkg>` |
| `  --imported-by` | `GET /v1/imported-by/<pkg>` |
| `pkginfo module <mod>` | `GET /v1/module/<mod>` |
| `  --readme` | `GET /v1/module/<mod>?readme=true` |
| `  --licenses` | `GET /v1/module/<mod>?licenses=true` |
| `  --versions` | `GET /v1/versions/<mod>` |
| `  --vulns` | `GET /v1/vulns/<mod>` |
| `  --packages` | `GET /v1/packages/<mod>` |
| `pkginfo search <q>` | `GET /v1/search/?q=<q>` |

The base command always makes one API call. Each additional flag adds at
most one more call. Flags that are query parameters on the same endpoint
(e.g., `--doc`, `--imports`, `--licenses` on a package) are combined into
a single request.

## Implementation

The tool lives in `cmd/internal/pkginfo/` within the pkgsite repository
(internal, until we finalize the CLI interface and get approved by
the Go proposal committee).  It is a
single binary with no dependencies beyond the Go standard library.
Response types are duplicated from `internal/api` rather than imported,
so the tool has zero third-party dependencies and can be moved to
another repository (e.g., `x/tools`) without pulling in pkgsite.

The implementation is intentionally thin: parse flags, construct HTTP
requests, decode JSON responses, format output. There is no client
library — the v1 API is the interface.

### Structure

```
cmd/internal/pkginfo/
  main.go          # flag parsing, subcommand dispatch, @version parsing
  fetch.go         # HTTP client: construct URLs, make requests, handle errors
  format.go        # text formatting for each entity type
```

### Error handling

API errors are returned as JSON with a `code` and `message` field.
In text mode, `pkginfo` prints the message to stderr and exits with a
non-zero status. In JSON mode, the error JSON is printed to stdout so
the caller can parse it.

### Exit codes

- 0: success
- 1: API error (not found, bad request, server error)
- 2: usage error (bad flags, missing arguments)

## Future directions

- **`go doc` integration**: Propose extending `go doc` with a `-remote`
  flag that uses the pkg.go.dev API for packages not available locally.
  Evidence from `pkginfo` usage would support the proposal.
- **gopls integration**: gopls could use the same API to surface version
  info, vulnerabilities, and imported-by counts in editor hover/code
  actions.
- **Private module servers**: The `--server` flag already supports
  pointing at a private pkgsite instance serving internal modules.
