// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// TestGolden parses every testdata/*.zap file, emits per-struct Go, and
// diffs each result against the corresponding *.go.golden fixture.
// Run with -update to regenerate goldens.
func TestGolden(t *testing.T) {
	matches, err := filepath.Glob("testdata/*.zap")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no testdata/*.zap files")
	}
	for _, in := range matches {
		in := in
		t.Run(filepath.Base(in), func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatalf("read %s: %v", in, err)
			}
			file, err := Parse(in, src)
			if err != nil {
				t.Fatalf("parse %s: %v", in, err)
			}
			emitted, err := Emit(file)
			if err != nil {
				t.Fatalf("emit %s: %v", in, err)
			}
			// Stable iteration: assert against every expected golden file.
			var names []string
			for n := range emitted {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, name := range names {
				got := emitted[name]
				goldenPath := filepath.Join("testdata", name+".golden")
				if *update {
					if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
						t.Fatalf("write golden %s: %v", goldenPath, err)
					}
					t.Logf("updated %s", goldenPath)
					continue
				}
				want, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Fatalf("read golden %s: %v (run with -update to create)", goldenPath, err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s: emitted output does not match golden\n--- got ---\n%s\n--- want ---\n%s",
						goldenPath, indent(got), indent(want))
				}
			}
		})
	}
}

// indent adds a two-space prefix to each line so the diff output in
// test failures is easier to read in the terminal.
func indent(b []byte) string {
	return "  " + strings.ReplaceAll(string(b), "\n", "\n  ")
}
