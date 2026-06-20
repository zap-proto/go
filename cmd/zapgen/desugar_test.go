// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"strings"
	"testing"
)

// TestDesugar pins the source-level transform: whitespace-significant
// input -> canonical brace output the existing parser accepts. Every
// input ends in '\n', so every want does too (final-newline preserved).
func TestDesugar(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "brace input is identity",
			in:   "package p\nstruct S {\n    A u8 @0\n}\n",
			want: "package p\nstruct S {\n    A u8 @0\n}\n",
		},
		{
			name: "header gets brace, fields get auto offsets",
			in:   "package p\nstruct S\n    A u8\n    B u32\n",
			want: "package p\nstruct S {\n    A u8 @0\n    B u32 @1\n}\n",
		},
		{
			name: "explicit offset preserved and resets cursor",
			in:   "package p\nstruct S\n    A u8 @4\n    B u32\n",
			want: "package p\nstruct S {\n    A u8 @4\n    B u32 @5\n}\n",
		},
		{
			name: "alias type sized for auto offset",
			in:   "package p\ntype id32 = bytes_fixed[32]\nstruct S\n    A id32\n    B u32\n",
			want: "package p\ntype id32 = bytes_fixed[32]\nstruct S {\n    A id32 @0\n    B u32 @32\n}\n",
		},
		{
			name: "list and bytes are 8-byte pointers",
			in:   "package p\nstruct S\n    L list<Foo>\n    M bytes\n    N u8\n",
			want: "package p\nstruct S {\n    L list<Foo> @0\n    M bytes @8\n    N u8 @16\n}\n",
		},
		{
			name: "nested struct pointer is 4 bytes",
			in:   "package p\nstruct S\n    F Foo\n    G u32\n",
			want: "package p\nstruct S {\n    F Foo @0\n    G u32 @4\n}\n",
		},
		{
			name: "blank lines and comments are transparent",
			in:   "package p\n\nstruct S\n    # leading comment\n    A u8\n\n    B u8\n",
			want: "package p\n\nstruct S {\n    # leading comment\n    A u8 @0\n\n    B u8 @1\n}\n",
		},
		{
			name: "two structs each reset the cursor",
			in:   "package p\nstruct A\n    X u32\nstruct B\n    Y u32\n",
			want: "package p\nstruct A {\n    X u32 @0\n}\nstruct B {\n    Y u32 @0\n}\n",
		},
		{
			name: "inline field comment stripped before offset, kept off output",
			in:   "package p\nstruct S\n    A u8  # the a field\n",
			want: "package p\nstruct S {\n    A u8 @0\n}\n",
		},
		{
			name: "brace and whitespace structs coexist in one file",
			in:   "package p\nstruct A {\n    X u8 @0\n}\nstruct B\n    Y u8\n",
			want: "package p\nstruct A {\n    X u8 @0\n}\nstruct B {\n    Y u8 @0\n}\n",
		},
		{
			name: "no trailing newline is preserved",
			in:   "package p\nstruct S\n    A u8",
			want: "package p\nstruct S {\n    A u8 @0\n}",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := Desugar([]byte(tc.in))
			if err != nil {
				t.Fatalf("Desugar: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Desugar mismatch\n--- got ---\n%q\n--- want ---\n%q", got, tc.want)
			}
		})
	}
}

// TestDesugarThenParse confirms desugared whitespace source is accepted
// by the real parser and yields the expected struct layout.
func TestDesugarThenParse(t *testing.T) {
	src := "package p\ntype id32 = bytes_fixed[32]\nstruct S\n    A u32\n    B id32\n    C bytes\n"
	f, err := Parse("s.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Structs) != 1 {
		t.Fatalf("want 1 struct, got %d", len(f.Structs))
	}
	s := f.Structs[0]
	want := []struct {
		name string
		off  int
	}{{"A", 0}, {"B", 4}, {"C", 36}}
	if len(s.Fields) != len(want) {
		t.Fatalf("want %d fields, got %d", len(want), len(s.Fields))
	}
	for i, w := range want {
		if s.Fields[i].Name != w.name || s.Fields[i].Offset != w.off {
			t.Errorf("field %d = (%s @%d), want (%s @%d)",
				i, s.Fields[i].Name, s.Fields[i].Offset, w.name, w.off)
		}
	}
}

// TestDesugarErrors covers malformed whitespace input.
func TestDesugarErrors(t *testing.T) {
	cases := map[string]string{
		"bad explicit offset":     "package p\nstruct S\n    A u8 @x\n",
		"unterminated fixed type": "package p\nstruct S\n    A bytes_fixed[\n",
	}
	for name, in := range cases {
		in := in
		t.Run(name, func(t *testing.T) {
			if _, err := Desugar([]byte(in)); err == nil {
				t.Errorf("want error, got nil")
			}
		})
	}
}

// TestDesugarKeywordNamedFields pins audit bugs H4 and H3: a struct field
// whose NAME is a keyword (`struct`, `interface`, `type`) must desugar as
// an ordinary field — not be misclassified as a block header (H4) and not
// crash the alias scanner (H3). A braceless header is EXACTLY
// `struct <Id>` + end-of-line; any type/`@offset` tail makes it a field.
func TestDesugarKeywordNamedFields(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// H4: field named `struct`/`interface` with an explicit offset is a
		// field, not a phantom nested block.
		{
			name: "H4 field named struct with explicit offset",
			in:   "package p\nstruct S\n    struct u8 @0\n",
			want: "package p\nstruct S {\n    struct u8 @0\n}\n",
		},
		{
			name: "H4 field named interface with explicit offset",
			in:   "package p\nstruct S\n    interface text @8\n",
			want: "package p\nstruct S {\n    interface text @8\n}\n",
		},
		// H4: `interface` is not a Go block opener, so a braceless
		// `interface text` is a field (auto offset), never a header.
		{
			name: "H4 field named interface auto offset",
			in:   "package p\nstruct S\n    interface text\n    B u8\n",
			want: "package p\nstruct S {\n    interface text @0\n    B u8 @8\n}\n",
		},
		// H3: field named `type` must not be scanned as a `type X = …` alias.
		{
			name: "H3 field named type with explicit offset",
			in:   "package p\nstruct S\n    type u8 @0\n",
			want: "package p\nstruct S {\n    type u8 @0\n}\n",
		},
		{
			name: "H3 field named type auto offset advances cursor",
			in:   "package p\nstruct S\n    type u32\n    B u8\n",
			want: "package p\nstruct S {\n    type u32 @0\n    B u8 @4\n}\n",
		},
		// H3 (brace form): collectAliases scanned brace bodies too, so even a
		// pure-brace struct with a `type`-named field used to crash.
		{
			name: "H3 field named type inside brace struct",
			in:   "package p\nstruct S {\n    type u8 @0\n}\n",
			want: "package p\nstruct S {\n    type u8 @0\n}\n",
		},
		// A real top-level alias must still be collected and size fields even
		// when a struct also has a `type`-named field (scope is respected).
		{
			name: "top-level alias coexists with type-named field",
			in:   "package p\ntype id32 = bytes_fixed[32]\nstruct S\n    type id32\n    B u8\n",
			want: "package p\ntype id32 = bytes_fixed[32]\nstruct S {\n    type id32 @0\n    B u8 @32\n}\n",
		},
		// `struct <Id>` with nothing after it is still a real header (parity
		// with the TS regex: keyword + one identifier + end-of-line).
		{
			name: "struct plus bare identifier is still a header",
			in:   "package p\nstruct S\n    A u8\n",
			want: "package p\nstruct S {\n    A u8 @0\n}\n",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := Desugar([]byte(tc.in))
			if err != nil {
				t.Fatalf("Desugar: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Desugar mismatch\n--- got ---\n%q\n--- want ---\n%q", got, tc.want)
			}
			// And the desugared form must actually parse.
			if _, err := Parse("t.zap", []byte(tc.in)); err != nil {
				t.Errorf("Parse(%q): %v", tc.in, err)
			}
		})
	}
}

// TestDesugarKeywordFieldParsed confirms a keyword-named field round-trips
// through the real parser with the right field name and offset (the H3/H4
// transform is only useful if the parser then accepts it). The `struct`
// field carries an explicit `@offset`: a bare `struct u8` is, by the
// keyword+one-identifier+end-of-line rule, a real header (struct named
// `u8`), so the `@N` tail is what marks this line as a field. `type` and
// `interface` are never block openers, so they need no disambiguator.
func TestDesugarKeywordFieldParsed(t *testing.T) {
	src := "package p\nstruct S\n    type u32\n    struct u8 @4\n    interface text\n"
	f, err := Parse("s.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Structs) != 1 {
		t.Fatalf("want 1 struct, got %d", len(f.Structs))
	}
	want := []struct {
		name string
		off  int
	}{{"type", 0}, {"struct", 4}, {"interface", 5}}
	s := f.Structs[0]
	if len(s.Fields) != len(want) {
		t.Fatalf("want %d fields, got %d", len(want), len(s.Fields))
	}
	for i, w := range want {
		if s.Fields[i].Name != w.name || s.Fields[i].Offset != w.off {
			t.Errorf("field %d = (%s @%d), want (%s @%d)",
				i, s.Fields[i].Name, s.Fields[i].Offset, w.name, w.off)
		}
	}
}

// TestDesugarOffsetOverflow pins audit bug H2: an `@offset` that overflows
// uint64 (or exceeds the sane bound) must be rejected with a clear error,
// never silently wrapped — a wrap to 0 aliases onto field offset 0 in the
// zero-copy layout.
func TestDesugarOffsetOverflow(t *testing.T) {
	cases := map[string]string{
		// 7766279631452241919 if accumulated in a signed int with no guard.
		"overflows uint64 (20 nines)": "package p\nstruct S\n    A u8 @99999999999999999999\n",
		// Exactly 2^64 — wraps to 0 in an unchecked accumulator.
		"wraps uint64 to zero (2^64)": "package p\nstruct S\n    A u8 @18446744073709551616\n",
		// Above maxOffset but within uint64 — still rejected by the bound.
		"exceeds max offset bound": "package p\nstruct S\n    A u8 @9999999999\n",
	}
	for name, in := range cases {
		in := in
		t.Run(name, func(t *testing.T) {
			_, err := Desugar([]byte(in))
			if err == nil {
				t.Fatalf("want overflow error, got nil")
			}
			if !strings.Contains(err.Error(), "out of range") {
				t.Errorf("want 'out of range' error, got %v", err)
			}
		})
	}
	// A large-but-legal offset (at the bound) must still parse.
	in := "package p\nstruct S\n    A u8 @2147483647\n" // maxOffset - 1
	if _, err := Desugar([]byte(in)); err != nil {
		t.Errorf("legal offset rejected: %v", err)
	}
}

// TestDesugarUnrecognizedHeaderPassesThrough pins audit bug M1: an
// unrecognized header followed by an indented body must NOT crash the
// desugar stage. The desugarer passes the lines through unchanged and the
// PARSER emits the precise top-level error (mirroring the TS behavior).
func TestDesugarUnrecognizedHeaderPassesThrough(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantOut   string // desugar output is byte-identical to input here
		parseErrs string // substring the parser error must contain
	}{
		{
			name:      "structFoo glued identifier then body",
			in:        "package p\nstructFoo\n    A u8\n",
			wantOut:   "package p\nstructFoo\n    A u8\n",
			parseErrs: "top level",
		},
		{
			name:      "bare struct keyword then body",
			in:        "package p\nstruct\n    A u8\n",
			wantOut:   "package p\nstruct\n    A u8\n",
			parseErrs: "expected", // parser reports the missing struct name/brace
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := Desugar([]byte(tc.in))
			if err != nil {
				t.Fatalf("Desugar must not error (parser owns the diagnostic): %v", err)
			}
			if string(got) != tc.wantOut {
				t.Errorf("Desugar should pass through unchanged\n--- got ---\n%q\n--- want ---\n%q", got, tc.wantOut)
			}
			_, perr := Parse("t.zap", []byte(tc.in))
			if perr == nil {
				t.Fatalf("Parse should reject, got nil")
			}
			if !strings.Contains(perr.Error(), tc.parseErrs) {
				t.Errorf("Parse error %q does not contain %q", perr.Error(), tc.parseErrs)
			}
		})
	}
}
