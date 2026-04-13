// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"slices"
	"testing"
)

func TestReorderArgs(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("json", false, "")
	fs.String("server", "", "")
	fs.Bool("symbols", false, "")
	fs.String("doc", "", "")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "flags before arg",
			args: []string{"--json", "--server=url", "pkg"},
			want: []string{"--json", "--server=url", "pkg"},
		},
		{
			name: "flags after arg",
			args: []string{"pkg", "--json", "--symbols"},
			want: []string{"--json", "--symbols", "pkg"},
		},
		{
			name: "mixed",
			args: []string{"--json", "pkg", "--symbols"},
			want: []string{"--json", "--symbols", "pkg"},
		},
		{
			name: "value flag after arg",
			args: []string{"pkg", "--server", "url"},
			want: []string{"--server", "url", "pkg"},
		},
		{
			name: "value flag with equals after arg",
			args: []string{"pkg", "--doc=text"},
			want: []string{"--doc=text", "pkg"},
		},
		{
			name: "no flags",
			args: []string{"pkg"},
			want: []string{"pkg"},
		},
		{
			name: "double dash stops",
			args: []string{"--", "pkg", "--json"},
			want: []string{"--", "pkg", "--json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderArgs(fs, tt.args)
			if !slices.Equal(got, tt.want) {
				t.Errorf("reorderArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
