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

// TestWhitespaceEquivalence proves the whitespace-significant syntax is a
// pure desugaring: the brace fixture and its indentation-only twin (no
// braces, no @N offsets — see testdata/ws/*.zap) generate byte-identical
// Go. Pairing is by basename: testdata/<x>.zap <-> testdata/ws/<x>_ws.zap.
func TestWhitespaceEquivalence(t *testing.T) {
	pairs := []struct{ brace, ws string }{
		{"testdata/basetx.zap", "testdata/ws/basetx_ws.zap"},
	}
	for _, pair := range pairs {
		pair := pair
		t.Run(filepath.Base(pair.ws), func(t *testing.T) {
			// Normalize the source basename on both sides so the only thing
			// compared is schema-derived output, not the `// source:` header
			// (which honestly differs: basetx.zap vs basetx_ws.zap).
			braceOut := emitAll(t, pair.brace, "schema.zap")
			wsOut := emitAll(t, pair.ws, "schema.zap")
			if len(braceOut) != len(wsOut) {
				t.Fatalf("file-count mismatch: brace=%d ws=%d", len(braceOut), len(wsOut))
			}
			for name, want := range braceOut {
				got, ok := wsOut[name]
				if !ok {
					t.Errorf("ws missing generated file %q", name)
					continue
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s: ws output != brace output\n--- ws ---\n%s\n--- brace ---\n%s",
						name, indent(got), indent(want))
				}
			}
		})
	}
}

// emitAll parses and emits one .zap fixture, overriding the recorded
// source basename to srcName so generated `// source:` headers match
// across fixtures. Fails the test on error.
func emitAll(t *testing.T, path, srcName string) map[string][]byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	file, err := Parse(path, src)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	file.Source = srcName
	out, err := Emit(file)
	if err != nil {
		t.Fatalf("emit %s: %v", path, err)
	}
	return out
}
