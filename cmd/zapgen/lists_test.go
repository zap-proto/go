// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.

package main

import (
	"strings"
	"testing"
)

// A list carries its elements one of three ways, and which one is a property
// of the element, not a flag: a struct with no tail lies inline at its own
// size, one with a tail is written as its own message behind a length, and
// ptr<T> is a run of signed offsets to objects written earlier in the same
// buffer. These assert that each shape emits the write the wire wants.
const threeLists = `package shapes

struct Flat {
    A u32 @0
    B u32 @4
}

struct Deep {
    Tag  u32   @0
    Note bytes @8
}

struct Holder {
    Records list<Flat>      @0
    Entries list<Deep>      @8
    Aimed   list<ptr<Deep>> @16
}
`

func emitBoth(t *testing.T, src string) (string, string) {
	t.Helper()
	f, err := Parse("shapes.zap", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, goBody, err := EmitSingle(f)
	if err != nil {
		t.Fatal(err)
	}
	files, err := EmitRust(f, defaultRustRuntime)
	if err != nil {
		t.Fatal(err)
	}
	return string(goBody), string(files["shapes_zap.rs"])
}

func TestListElementShapes(t *testing.T) {
	goSrc, rustSrc := emitBoth(t, threeLists)
	// gofmt aligns neighbouring one-line functions, so compare the Go half
	// with its runs of spaces squeezed out.
	goSrc = strings.Join(strings.Fields(goSrc), " ")
	for _, want := range []string{
		"recordsLB.AddBytes(elem)",
		"entriesLB.AddObjectBytes(elem)",
		"aimedLB.AddObjectPtr(at)",
		"func (t Holder) AimedAt(i int) Deep { return Deep{o: t.Aimed().ObjectPtr(i)} }",
		"func (t Holder) Records() zap.List { return t.o.ListStride(holderRecordsOff, 8) }",
		"func (t Holder) Entries() zap.List { return t.o.ListStride(holderEntriesOff, 4) }",
		"func (t Holder) Aimed() zap.List { return t.o.ListStride(holderAimedOff, 4) }",
		"offsAimed = append(offsAimed, PutDeep(b, elem))",
		"Aimed []DeepInput",
		"func (t Holder) RecordsAt(i int) Flat { return Flat{o: t.Records().Object(i, flatSize)} }",
		"func (t Holder) EntriesAt(i int) Deep { return Deep{o: t.Entries().ObjectAt(i)} }",
	} {
		if !strings.Contains(goSrc, want) {
			t.Errorf("Go emit is missing %q\n%s", want, goSrc)
		}
	}
	for _, want := range []string{
		"list_records.add_bytes(b, elem);",
		"list_entries.add_object_bytes(b, elem);",
		"list_aimed.add_object_ptr(b, *at);",
		"Deep::new(self.aimed().object_ptr(i))",
		"self.o.list_stride(HOLDER_RECORDS, 8)",
		"self.o.list_stride(HOLDER_ENTRIES, 4)",
		"offs_aimed.push(put_deep(b, elem));",
		"pub aimed: &'a [DeepInput<'a>],",
		"Flat::new(self.records().object(i, FLAT_SIZE))",
		"Deep::new(self.entries().object_at(i))",
	} {
		if !strings.Contains(rustSrc, want) {
			t.Errorf("Rust emit is missing %q\n%s", want, rustSrc)
		}
	}
}

// The record form exists only for a struct that has no tail: those are the
// bytes one element of an inline list holds.
func TestPackIsForInlineStructsOnly(t *testing.T) {
	goSrc, rustSrc := emitBoth(t, threeLists)
	if !strings.Contains(goSrc, "func PackFlat(in FlatInput) [flatSize]byte {") {
		t.Errorf("Go emit has no PackFlat\n%s", goSrc)
	}
	if strings.Contains(goSrc, "func PackDeep") {
		t.Error("Go emit packs a struct that has a tail")
	}
	if !strings.Contains(rustSrc, "pub fn pack_flat(input: &FlatInput) -> [u8; FLAT_SIZE] {") {
		t.Errorf("Rust emit has no pack_flat\n%s", rustSrc)
	}
	if strings.Contains(rustSrc, "pub fn pack_deep") {
		t.Error("Rust emit packs a struct that has a tail")
	}
}

// What a pointer names is written before the pointer, and the fixed section
// after both — the order the reference wire is in.
func TestTheFixedSectionIsWrittenLast(t *testing.T) {
	goSrc, rustSrc := emitBoth(t, threeLists)
	for name, src := range map[string]string{"Go": goSrc, "Rust": rustSrc} {
		put := src[strings.Index(src, "Holder"):]
		leaf := strings.Index(put, "PutDeep(b, elem)")
		if leaf < 0 {
			leaf = strings.Index(put, "put_deep(b, elem)")
		}
		list := strings.Index(put, "AddObjectPtr")
		if list < 0 {
			list = strings.Index(put, "add_object_ptr")
		}
		object := strings.Index(put, "StartObject")
		if object < 0 {
			object = strings.Index(put, "start_object")
		}
		if !(leaf >= 0 && leaf < list && list < object) {
			t.Errorf("%s: writes are out of order: leaf=%d list=%d object=%d", name, leaf, list, object)
		}
	}
}

// ptr<T> says how a list holds its elements. A field that holds one struct
// is already a pointer, so there is one way to write that and it is not this.
func TestPtrOutsideAListIsRefused(t *testing.T) {
	f, err := Parse("bad.zap", []byte("package bad\n\nstruct A {\n    X u32 @0\n}\n\nstruct B {\n    P ptr<A> @0\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EmitSingle(f); err == nil {
		t.Fatal("a ptr<> field was accepted")
	} else if !strings.Contains(err.Error(), "already a pointer") {
		t.Fatalf("unhelpful error: %v", err)
	}
}
