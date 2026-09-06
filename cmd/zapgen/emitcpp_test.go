// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGoldenCPP emits per-struct C++ for every testdata schema and diffs each
// result against its fixture, exactly as TestGolden does for Go. Run with
// -update to regenerate.
func TestGoldenCPP(t *testing.T) {
	matches, err := filepath.Glob("testdata/*.zap")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no testdata/*.zap files")
	}
	for _, in := range matches {
		t.Run(filepath.Base(in), func(t *testing.T) {
			file := parseFile(t, in)
			emitted, err := EmitCPP(file)
			if err != nil {
				t.Fatalf("emit %s: %v", in, err)
			}
			var names []string
			for n := range emitted {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, name := range names {
				golden := filepath.Join("testdata", name+".golden")
				if *update {
					if err := os.WriteFile(golden, emitted[name], 0o644); err != nil {
						t.Fatalf("write golden %s: %v", golden, err)
					}
					t.Logf("updated %s", golden)
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("read golden %s: %v (run with -update)", golden, err)
				}
				if string(want) != string(emitted[name]) {
					t.Errorf("%s does not match golden", name)
				}
			}
		})
	}
}

// TestBackendsShareOneModel is the property that makes two backends safe: they
// read the same AST, so a field's offset and a method's ordinal appear in the
// C++ output exactly where the Go output has them. If the two ever drifted,
// this is where it would show — no schema needs to be written twice.
func TestBackendsShareOneModel(t *testing.T) {
	file := parseFile(t, "testdata/basetx.zap")
	goSrc, _, err := emitSingleString(EmitSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	cppSrc, _, err := emitSingleString(EmitCPPSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	// gofmt aligns the Go const block, so compare on collapsed whitespace.
	goSrc, cppSrc = collapse(goSrc), collapse(cppSrc)
	for _, f := range file.Structs[0].Fields {
		goWant := fmt.Sprintf("baseTx%sOff = %d", f.Name, f.Offset)
		cppWant := fmt.Sprintf("kBaseTx%sOff = %d", f.Name, f.Offset)
		if !strings.Contains(goSrc, goWant) {
			t.Errorf("Go output missing %q", goWant)
		}
		if !strings.Contains(cppSrc, cppWant) {
			t.Errorf("C++ output missing %q", cppWant)
		}
	}

	echo := parseFile(t, "testdata/echo.zap")
	goSrc, _, err = emitSingleString(EmitSingle, echo)
	if err != nil {
		t.Fatal(err)
	}
	cppSrc, _, err = emitSingleString(EmitCPPSingle, echo)
	if err != nil {
		t.Fatal(err)
	}
	goSrc, cppSrc = collapse(goSrc), collapse(cppSrc)
	for _, m := range echo.Interfaces[0].Methods {
		name := ordinalName(echo.Interfaces[0], m)
		if !strings.Contains(goSrc, fmt.Sprintf("%s uint32 = %d", name, m.Ordinal)) {
			t.Errorf("Go output missing ordinal %s = %d", name, m.Ordinal)
		}
		if !strings.Contains(cppSrc, fmt.Sprintf("k%s = %d", name, m.Ordinal)) {
			t.Errorf("C++ output missing ordinal k%s = %d", name, m.Ordinal)
		}
	}
}

// TestCPPWritesTheSameWireVersionAsGo pins the one place the two runtimes
// disagree by default: zap.NewBuilder writes version 1 and zap::Builder writes
// version 2, so the generated C++ has to name the version or the two builders
// emit different headers from one schema.
func TestCPPWritesTheSameWireVersionAsGo(t *testing.T) {
	file := parseFile(t, "testdata/basetx.zap")
	src, _, err := emitSingleString(EmitCPPSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "zap::Builder b(256, zap::kVersion1)") {
		t.Error("generated C++ builder does not pin the Go builder's wire version")
	}
}

// TestCPPFixedFieldIsAlwaysItsWidth guards the divergence the corpus caught:
// Go's [N]byte accessor answers N zero bytes for a buffer too short to hold
// the field, so the C++ span accessor has to answer N zero bytes too.
func TestCPPFixedFieldIsAlwaysItsWidth(t *testing.T) {
	file := parseFile(t, "testdata/basetx.zap")
	src, _, err := emitSingleString(EmitCPPSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "std::array<std::uint8_t, 32> zero{}") ||
		!strings.Contains(src, "b.size() == 32 ? b : std::span<const std::uint8_t>(zero)") {
		t.Error("generated C++ bytes_fixed accessor is not total in shape")
	}
}

// TestCPPNestedStructOrderIsFree checks that a struct may name one declared
// after it: the classes are forward-declared and the accessors that return one
// are defined after every class body, under their qualified name so a field
// may carry the name of its own type.
func TestCPPNestedStructOrderIsFree(t *testing.T) {
	src := []byte(`package p
struct A {
    Child B @0
}
struct B {
    N u32 @0
}`)
	file, err := Parse("order.zap", src)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := emitSingleString(EmitCPPSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "class A;\nclass B;") {
		t.Error("classes are not forward-declared")
	}
	want := "inline ::p::B A::Child() const { return ::p::B(o_.object(kAChildOff)); }"
	if !strings.Contains(out, want) {
		t.Error("nested accessor is not defined out of line, qualified")
	}
	if strings.Index(out, "class B {") > strings.Index(out, want) {
		t.Error("nested accessor is defined before the class it returns")
	}
}

// TestUnknownLangIsRefused keeps the backend table honest: an unknown -lang is
// an error, not a silent fallback to Go.
func TestUnknownLangIsRefused(t *testing.T) {
	f := parseFile(t, "testdata/echo.zap")
	if _, err := emit(f, "fortran", defaultRustRuntime, true); err == nil {
		t.Error("an unknown language should be refused, not silently emitted as Go")
	}
	for _, lang := range []string{"go", "cpp", "rust"} {
		if _, err := emit(f, lang, defaultRustRuntime, true); err != nil {
			t.Errorf("emit(%q): %v", lang, err)
		}
	}
}

// collapse squeezes runs of spaces and tabs to one space, so an assertion
// about what a line says does not also assert how gofmt aligned it.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func parseFile(t *testing.T, path string) *File {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	file, err := Parse(path, src)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

func emitSingleString(emit func(*File) (string, []byte, error), f *File) (string, string, error) {
	name, body, err := emit(f)
	return string(body), name, err
}

// TestWhitespaceEquivalenceCPP is TestWhitespaceEquivalence's other half: the
// brace fixture and its indentation-only twin generate byte-identical C++ too.
// The desugar is shared, so this is what it means for it to be shared.
func TestWhitespaceEquivalenceCPP(t *testing.T) {
	brace := emitAllCPP(t, "testdata/basetx.zap", "schema.zap")
	ws := emitAllCPP(t, "testdata/ws/basetx_ws.zap", "schema.zap")
	if len(brace) != len(ws) {
		t.Fatalf("file-count mismatch: brace=%d ws=%d", len(brace), len(ws))
	}
	for name, want := range brace {
		got, ok := ws[name]
		if !ok {
			t.Errorf("ws missing generated file %q", name)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s: ws C++ output != brace C++ output", name)
		}
	}
}

func emitAllCPP(t *testing.T, path, srcName string) map[string][]byte {
	t.Helper()
	file := parseFile(t, path)
	file.Source = srcName
	out, err := EmitCPP(file)
	if err != nil {
		t.Fatalf("emit cpp %s: %v", path, err)
	}
	return out
}
