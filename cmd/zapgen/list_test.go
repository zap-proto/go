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
