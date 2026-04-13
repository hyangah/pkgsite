// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// A command describes a subcommand of pkginfo.
type command struct {
	name    string // subcommand name
	args    string // e.g. "<package>[@version]"; empty for no-arg commands
	summary string // one-line description
	flags   *flag.FlagSet
	run     func(fs *flag.FlagSet, stdout, stderr io.Writer) int
}

func (c *command) usageLine() string {
	parts := []string{"pkginfo", c.name}
	if c.args != "" {
		parts = append(parts, c.args, "[flags]")
	}
	return strings.Join(parts, " ")
}

// printCommandUsage writes usage for a single command to w.
func printCommandUsage(w io.Writer, c *command) {
	fmt.Fprintf(w, "Usage: %s\n", c.usageLine())
	if c.flags != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		c.flags.SetOutput(w)
		c.flags.PrintDefaults()
	}
}

// printUsage writes usage for all commands to w.
func printUsage(w io.Writer, cmds []*command) {
	fmt.Fprintln(w, "Usage:")
	for i, c := range cmds {
		line := c.usageLine()
		if i == 0 {
			line += " (default)"
		}
		fmt.Fprintf(w, "  %-50s %s\n", line, c.summary)
	}
	fmt.Fprintf(w, "\nRun \"pkginfo <command> -h\" for command-specific flags.\n")
}

// dispatch finds and runs the matching command. It returns the exit code.
func dispatch(args []string, cmds []*command, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr, cmds)
		return 2
	}

	// Bare "-h" with no positional arg: show overview.
	hasPositional := false
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			hasPositional = true
			break
		}
	}
	if !hasPositional {
		printUsage(stdout, cmds)
		return 0
	}

	named := map[string]*command{}
	for _, c := range cmds {
		named[c.name] = c
	}

	// Scan for the first positional arg to determine the subcommand.
	// If it doesn't match a known command, fall back to the first
	// command (package) as the default.
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if c, ok := named[a]; ok {
			rest := append(args[:i:i], args[i+1:]...)
			return parseAndRun(c, rest, stdout, stderr)
		}
		// Not a subcommand name — treat as default (package) command.
		return parseAndRun(cmds[0], args, stdout, stderr)
	}

	printUsage(stderr, cmds)
	return 2
}

func parseAndRun(c *command, args []string, stdout, stderr io.Writer) int {
	if c.flags == nil {
		// No-arg command (help, version).
		return c.run(nil, stdout, stderr)
	}
	c.flags.SetOutput(stderr)
	c.flags.Usage = func() { printCommandUsage(stderr, c) }
	if err := c.flags.Parse(reorderArgs(c.flags, args)); err != nil {
		return 2
	}
	return c.run(c.flags, stdout, stderr)
}

// reorderArgs moves flags before positional arguments so that
// flag.Parse handles them correctly. This allows users to write
// flags in any position, e.g. "pkginfo encoding/json -symbols".
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		// If this flag takes a value as a separate arg (not --flag=val),
		// consume the next arg too.
		if !strings.Contains(a, "=") {
			name := strings.TrimLeft(a, "-")
			if f := fs.Lookup(name); f != nil && !isBoolFlag(f) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	return append(flags, positional...)
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

func versionInfo() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "pkginfo (unknown version)"
	}
	v := bi.Main.Version
	if v == "" || v == "(devel)" {
		v = "devel"
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 12 {
				v += " " + s.Value[:12]
			}
		}
	}
	return "pkginfo " + v + " " + bi.GoVersion
}
