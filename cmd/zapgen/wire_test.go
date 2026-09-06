// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"strings"
	"testing"
)

// The schema every test here reads. It holds one list of each shape the wire
// has, so the rule that decides between them is exercised by all of them at
// once rather than by the one a chain happens to use.
const shapes = `package s

struct Rec {
    A u32            @0
    B bytes_fixed[8] @4
}

struct Tailed {
    A u32  @0
    B text @4
}

struct Holder {
    Records list<Rec>    @0
    Entries list<Tailed> @8
    Blobs   list<bytes>  @16
    Words   list<text>   @24
    Counts  list<u32>    @32
    Keys    list<bytes_fixed[20]> @40
}
`

func shapeFile(t *testing.T) *File {
	t.Helper()
	f, err := Parse("shapes.zap", []byte(shapes))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

// TestShapeIsDecidedByTheElement — the element decides how the list carries
// it, and nothing in the schema says so out loud. A record has a width, so it
// is written at that width; anything without one is written behind a length.
func TestShapeIsDecidedByTheElement(t *testing.T) {
	f := shapeFile(t)
	var holder *Struct
	for _, s := range f.Structs {
		if s.Name == "Holder" {
			holder = s
		}
	}
	if holder == nil {
		t.Fatal("no Holder")
	}
	want := map[string]int{
		"Records": 12, // u32 + 8 inline bytes
		"Entries": 0,  // a text tail: no width to write at
		"Blobs":   0,
		"Words":   0,
		"Counts":  4,
		"Keys":    20,
	}
	for _, fd := range holder.Fields {
		if got := fd.Type.Stride; got != want[fd.Name] {
			t.Errorf("%s stride = %d, want %d", fd.Name, got, want[fd.Name])
		}
	}
}

// TestUnwritableListsAreRefused — a list whose element the file does not
// declare, and a list of lists, have no shape on the wire. Picking one for
// them quietly is how a schema comes to mean whichever backend read it.
func TestUnwritableListsAreRefused(t *testing.T) {
	for _, src := range []string{
		"package s\n\nstruct A {\n    X list<Elsewhere> @0\n}\n",
		"package s\n\nstruct A {\n    X list<list<u32>> @0\n}\n",
	} {
		if _, err := Parse("bad.zap", []byte(src)); err == nil {
			t.Errorf("accepted a list with no shape:\n%s", src)
		}
	}
}

// TestStrideListIsWrittenAtItsWidth — the emitted builder lays records back
// to back at the width the schema states, and passes the ELEMENT count, not
// the byte count, to the pointer.
func TestStrideListIsWrittenAtItsWidth(t *testing.T) {
	out := emitGo(t, shapeFile(t))
	for _, want := range []string{
		"lb := b.StartList(recSize)",
		"var rec [recSize]byte",
		"copy(rec[:], elem)",
		"lb.AddBytes(rec[:])",
		"ob.SetList(holderRecordsOff, recordsAt, len(in.Records))",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("emitted Go does not write a stride list: missing %q", want)
		}
	}
	if strings.Contains(out, "recordsAt = lb.FinishOffset()") == false {
		t.Error("the list's offset is not carried to the object")
	}
}

// TestTailedElementKeepsItsLength — a struct with a tail cannot be a record,
// so its list keeps the length-prefixed shape. Writing it at a stride would
// drop every tail silently.
func TestTailedElementKeepsItsLength(t *testing.T) {
	out := emitGo(t, shapeFile(t))
	if !strings.Contains(out, "lb.AddObjectBytes(elem)") {
		t.Error("a tailed element is not written behind its length")
	}
	if !strings.Contains(out, "func (x TailedList) At(i int) Tailed { return Tailed{o: x.l.ObjectAt(i)} }") {
		t.Error("a tailed element is not read behind its length")
	}
}

// TestListAnswersItsElementType — the accessor a caller reaches for is typed,
// so `list.Object(i, SIZE)` is not written by hand anywhere.
func TestListAnswersItsElementType(t *testing.T) {
	out := emitGo(t, shapeFile(t))
	for _, want := range []string{
		"func (t Holder) Records() RecList { return RecList{l: t.o.ListStride(holderRecordsOff, recSize)} }",
		"func (x RecList) At(i int) Rec { return Rec{o: x.l.Object(i, recSize)} }",
		"func (t Rec) Record() []byte { return t.o.BytesFixed(0, recSize) }",
	} {
		if !strings.Contains(collapse(out), collapse(want)) {
			t.Errorf("missing %q", want)
		}
	}
	// A list of something with no type of its own answers the runtime's list.
	if !strings.Contains(out, "func (t Holder) Blobs() zap.List") {
		t.Error("a list of bytes should answer the runtime's own list")
	}
}

// TestReadIsTightWhenTheWidthIsKnown — the generated reader asks for the
// bound the schema affords. A list of bytes cannot ask for one.
func TestReadIsTightWhenTheWidthIsKnown(t *testing.T) {
	out := emitGo(t, shapeFile(t))
	if !strings.Contains(out, "t.o.ListStride(holderCountsOff, 4)") {
		t.Error("a run of scalars is not read at its width")
	}
	if !strings.Contains(out, "t.o.List(holderBlobsOff)") {
		t.Error("a list of bytes should be read without a stride it does not have")
	}
}

// TestTailsAreWrittenBeforeTheObject — the chains write what a pointer names
// first and the object last, and the emitted builder now does too. The order
// is the difference between the same fields and the same bytes.
func TestTailsAreWrittenBeforeTheObject(t *testing.T) {
	out := emitGo(t, shapeFile(t))
	body := out[strings.Index(out, "func NewHolder("):]
	list := strings.Index(body, "b.StartList(")
	object := strings.Index(body, "b.StartObject(")
	if list < 0 || object < 0 {
		t.Fatal("no list and object in the emitted builder")
	}
	if list > object {
		t.Error("the object is written before what its pointers name")
	}
}

func emitGo(t *testing.T, f *File) string {
	t.Helper()
	_, body, err := EmitSingle(f)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return string(body)
}

// TestEveryBackendStampsTheSameVersion — the wire version is spelled, not
// defaulted, because the runtimes default differently: zap.NewBuilder writes
// version 1 and zap::Builder writes version 2. A generated builder that took
// either default would put a different header on one schema in each language.
// Two is what every Lux message on the wire carries, so two is what every
// backend stamps.
func TestEveryBackendStampsTheSameVersion(t *testing.T) {
	f := shapeFile(t)
	for _, tc := range []struct{ lang, want string }{
		{"go", "zap.NewBuilderV2(256)"},
		{"rust", "zap::Builder::new_v2(256)"},
		{"cpp", "zap::Builder b(256, zap::kVersion2)"},
	} {
		files, err := emit(f, tc.lang, defaultRustRuntime, true)
		if err != nil {
			t.Fatalf("emit %s: %v", tc.lang, err)
		}
		found := false
		for _, body := range files {
			if strings.Contains(string(body), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("the %s backend does not stamp %q", tc.lang, tc.want)
		}
	}
}

// TestEveryBackendWritesTheSameStride — the shape is decided once, in the
// front end, so a backend cannot reach a different answer. Each spells the
// width in its own idiom and every spelling names the element's own size.
func TestEveryBackendWritesTheSameStride(t *testing.T) {
	f := shapeFile(t)
	for _, tc := range []struct{ lang, stride, prefixed string }{
		{"go", "b.StartList(recSize)", "lb.AddObjectBytes(elem)"},
		{"rust", "let mut rec = [0u8; REC_SIZE];", "lb.add_object_bytes(&mut b, elem);"},
		{"cpp", "b.start_list(kRecSize)", "lb.add_u32(static_cast<std::uint32_t>(elem.size()));"},
	} {
		files, err := emit(f, tc.lang, defaultRustRuntime, true)
		if err != nil {
			t.Fatalf("emit %s: %v", tc.lang, err)
		}
		var all string
		for _, body := range files {
			all += string(body)
		}
		if !strings.Contains(all, tc.stride) {
			t.Errorf("the %s backend does not write a record at its own width: want %q", tc.lang, tc.stride)
		}
		if !strings.Contains(all, tc.prefixed) {
			t.Errorf("the %s backend does not write a tailed element behind its length: want %q", tc.lang, tc.prefixed)
		}
	}
}

// TestEveryBackendEmbedsANestedStruct — a nested-struct field points at an
// object, and the reader reads the child's fields at the child's offsets from
// there. A builder handed a message the caller built already has to copy it,
// and the pointer it writes has to name where the child's ROOT landed:
// aimed at the head of the copy it names the copy's 16-byte header, and the
// first field reads back as 0x0050415A, which is "ZAP".
//
// Two backends answered that differently once. They answer it the same way
// now, and this is where that is said.
func TestEveryBackendEmbedsANestedStruct(t *testing.T) {
	f, err := Parse("nest.zap", []byte(`package n

struct Child {
    N u32 @0
}

struct Parent {
    Kid Child @0
}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ lang, want string }{
		{"go", "kidAt := b.Embed(in.Kid)"},
		{"rust", "let at_kid = b.embed(input.kid);"},
		{"cpp", "const std::int64_t kid_at = b.embed(in.Kid);"},
	} {
		files, err := emit(f, tc.lang, defaultRustRuntime, true)
		if err != nil {
			t.Fatalf("emit %s: %v", tc.lang, err)
		}
		var all string
		for _, body := range files {
			all += string(body)
		}
		if !strings.Contains(all, tc.want) {
			t.Errorf("the %s backend does not embed a nested struct: want %q\n%s", tc.lang, tc.want, all)
		}
		// The shape it must NOT have: a fresh object holding the copy, whose
		// pointer names the copy's header.
		if strings.Contains(all, "nested.SetBytesFixed(0,") || strings.Contains(all, "nested.set_bytes_fixed(0,") {
			t.Errorf("the %s backend still points at the head of the copy", tc.lang)
		}
	}
}
