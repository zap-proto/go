// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import "testing"

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
