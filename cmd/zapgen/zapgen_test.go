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

// TestARepeatedFieldNameIsRefused — a struct that declares one name twice is
// not a schema, and the front end says so rather than handing every backend a
// collision to discover in its own language.
//
// The P-chain hit this: an envelope's owner-address pool and the owner group a
// transaction hands ownership TO were both called OwnerAddrs, and the emitted
// Rust had a struct with the same member twice — source that does not compile,
// found by rustc instead of by the line that wrote it.
func TestARepeatedFieldNameIsRefused(t *testing.T) {
	const src = `package p

struct Two {
    Addrs list<u32> @0
    Other u32       @8
    Addrs list<u32> @12
}
`
	_, err := Parse("two.zap", []byte(src))
	if err == nil {
		t.Fatal("a struct declaring Addrs twice was accepted")
	}
	if want := "struct Two declares Addrs twice"; !strings.Contains(err.Error(), want) {
		t.Errorf("error does not name the repeat: %v", err)
	}
	// The line the SECOND one is on, which is where the fix goes.
	if !strings.Contains(err.Error(), "two.zap:6") {
		t.Errorf("error does not name the line: %v", err)
	}
}

// TestTwoStructsMayShareAFieldName — the refusal is per struct. Every P-chain
// transaction repeats the same eight envelope fields, so a rule that reached
// across structs would refuse the schema this generator exists for.
func TestTwoStructsMayShareAFieldName(t *testing.T) {
	const src = `package p

struct A {
    Addrs list<u32> @0
}

struct B {
    Addrs list<u32> @0
}
`
	if _, err := Parse("two.zap", []byte(src)); err != nil {
		t.Fatalf("two structs naming one field each were refused: %v", err)
	}
}

// TestAStructNamedButNotDeclaredIsRefused — one schema is one closed set of
// names. A field whose type lives in some other file reaches the emitted
// source as a dangling type, and the author hears about it from rustc,
// pointing at generated code nobody wrote.
//
// Found trying to lift the fx primitives out of the X-chain schema: the
// X-chain's own envelope holds `list<ptr<TransferableOut>>`, so moving that
// struct away left the reference behind, and the generator emitted it anyway.
func TestAStructNamedButNotDeclaredIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, field string }{
		{"nested", "Leaf Leaf @0"},
		{"pointer", "Leaf ptr<Leaf> @0"},
		{"list of pointers", "Leaves list<ptr<Leaf>> @0"},
		{"list of structs", "Leaves list<Leaf> @0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\n\nstruct Holder {\n    " + tc.field + "\n}\n"
			_, err := Parse("holder.zap", []byte(src))
			if err == nil {
				t.Fatal("a field naming an undeclared struct was accepted")
			}
			if !strings.Contains(err.Error(), "which this schema does not declare") {
				t.Errorf("error does not say what is wrong: %v", err)
			}
			if !strings.Contains(err.Error(), "Leaf") {
				t.Errorf("error does not name the missing struct: %v", err)
			}
		})
	}
}

// TestAStructMayBeNamedBeforeItIsDeclared — the check is over the whole file,
// not the prefix parsed so far. A schema reads top to bottom and its structs
// do not have to.
func TestAStructMayBeNamedBeforeItIsDeclared(t *testing.T) {
	const src = `package p

struct Holder {
    Leaves list<ptr<Leaf>> @0
}

struct Leaf {
    Tag u32 @0
}
`
	if _, err := Parse("holder.zap", []byte(src)); err != nil {
		t.Fatalf("a forward reference inside one schema was refused: %v", err)
	}
}
