// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// A command describes a subcommand of pkginfo.
type command struct {
	name    string // "" for the default (package) command
	args    string // e.g. "<package>[@version]"
	summary string // one-line description
	flags   *flag.FlagSet
	run     func(fs *flag.FlagSet, stdout, stderr io.Writer) int
}

func (c *command) usageLine() string {
	prefix := "pkginfo"
	if c.name != "" {
		prefix += " " + c.name
	}
	return fmt.Sprintf("%s [flags] %s", prefix, c.args)
}

// printCommandUsage writes usage for a single command to w.
func printCommandUsage(w io.Writer, c *command) {
	fmt.Fprintf(w, "Usage: %s\n\n", c.usageLine())
	fmt.Fprintf(w, "Flags:\n")
	c.flags.SetOutput(w)
	c.flags.PrintDefaults()
}

// printUsage writes usage for all commands to w.
func printUsage(w io.Writer, cmds []*command) {
	fmt.Fprintln(w, "Usage:")
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-45s %s\n", c.usageLine(), c.summary)
	}
	fmt.Fprintf(w, "\nRun \"pkginfo <command> -h\" for command-specific flags.\n")
}

// dispatch finds and runs the matching command. It returns the exit code.
func dispatch(args []string, cmds []*command, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr, cmds)
		return 2
	}

	// "help" with no subcommand prints the overview.
	if args[0] == "help" {
		printUsage(stdout, cmds)
		return 0
	}

	// Find the default command (name == "").
	var defaultCmd *command
	named := map[string]*command{}
	for _, c := range cmds {
		if c.name == "" {
			defaultCmd = c
		} else {
			named[c.name] = c
		}
	}

	// Scan for the first positional arg to determine the subcommand.
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if c, ok := named[a]; ok {
			rest := append(args[:i:i], args[i+1:]...)
			return parseAndRun(c, rest, stdout, stderr)
		}
		if defaultCmd != nil {
			return parseAndRun(defaultCmd, args, stdout, stderr)
		}
		fmt.Fprintf(stderr, "unknown command: %s\n", a)
		printUsage(stderr, cmds)
		return 2
	}

	// Only flags, no positional arg — try the default command
	// (it will show its own usage if args are wrong).
	if defaultCmd != nil {
		return parseAndRun(defaultCmd, args, stdout, stderr)
	}
	printUsage(stderr, cmds)
	return 2
}

func parseAndRun(c *command, args []string, stdout, stderr io.Writer) int {
	c.flags.SetOutput(stderr)
	c.flags.Usage = func() { printCommandUsage(stderr, c) }
	if err := c.flags.Parse(args); err != nil {
		return 2
	}
	return c.run(c.flags, stdout, stderr)
}
