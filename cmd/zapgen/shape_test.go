// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2026, Lux Industries Inc. All rights reserved.

package main

import (
	"strings"
	"testing"
)

// TestShapeFollowsTheElementType is the rule that lets a schema stay silent
// about encoding: what a list looks like on the wire is decided by what is in
// it, so two authors cannot spell the same list two ways.
func TestShapeFollowsTheElementType(t *testing.T) {
	file := parseFile(t, "testdata/basetx.zap")
	cases := []struct {
		field  string
		owner  string
		shape  Shape
		stride int
	}{
		{"Outs", "BaseTx", ShapePointer, 4},   // Transfer carries a bytes tail
		{"Ins", "BaseTx", ShapePointer, 4},    // so does Spend
		{"Addrs", "Owner", ShapeFixed, 20},    // bytes_fixed[20]
		{"Sigs", "Owner", ShapeNumber, 4},     // u32
		{"Entries", "Owner", ShapeInline, 44}, // Entry has no tail
	}
	for _, c := range cases {
		s := file.Struct(c.owner)
		if s == nil {
			t.Fatalf("no struct %s", c.owner)
		}
		var f *Field
		for _, cand := range s.Fields {
			if cand.Name == c.field {
				f = cand
			}
		}
		if f == nil {
			t.Fatalf("%s has no field %s", c.owner, c.field)
		}
		elem := *f.Type.ListElem
		if got := file.Shape(elem); got != c.shape {
			t.Errorf("%s.%s shape = %d, want %d", c.owner, c.field, got, c.shape)
		}
		if got := file.Stride(elem); got != c.stride {
			t.Errorf("%s.%s stride = %d, want %d", c.owner, c.field, got, c.stride)
		}
	}
}

// TestPayloadsComeBeforeTheObject pins the order the Lux reference writes in.
// A list run is written first and the object second, because the object's
// pointer field needs an offset that already exists — and because writing the
// object first would move every byte after it.
func TestPayloadsComeBeforeTheObject(t *testing.T) {
	file := parseFile(t, "testdata/basetx.zap")
	for _, emit := range []func(*File) (string, []byte, error){EmitSingle, EmitCPPSingle} {
		src, _, err := emitSingleString(emit, file)
		if err != nil {
			t.Fatal(err)
		}
		// The definition, not the declaration that precedes it in C++.
		list := strings.LastIndex(src, "AppendOwner")
		if list < 0 {
			t.Fatal("no AppendOwner in the emitted source")
		}
		body := src[list:]
		run := strings.Index(body, "start_list(20)")
		if run < 0 {
			run = strings.Index(body, "StartList(20)")
		}
		object := strings.Index(body, "start_object(")
		if object < 0 {
			object = strings.Index(body, "StartObject(")
		}
		if run < 0 || object < 0 {
			t.Fatal("emitted AppendOwner writes neither a list nor an object")
		}
		if run > object {
			t.Error("the emitted builder opens the object before it writes the list run")
		}
	}
}

// TestAListOfSomethingWithNoStrideIsRefused: bytes and text have no width, so
// a run of them has no encoding. Saying so once, in the model, is what stops
// each backend inventing a different one.
func TestAListOfSomethingWithNoStrideIsRefused(t *testing.T) {
	src := `package p
struct S {
    Runs list<bytes> @0
}`
	file, err := Parse("t.zap", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EmitCPPSingle(file); err == nil {
		t.Error("emitted C++ for a list<bytes>")
	}
	if _, _, err := EmitSingle(file); err == nil {
		t.Error("emitted Go for a list<bytes>")
	}
}

// TestAnUndeclaredElementIsRefused: the shape of a list of structs depends on
// what the struct holds, so a struct that is not there is not a shape.
func TestAnUndeclaredElementIsRefused(t *testing.T) {
	src := `package p
struct S {
    Kids list<Missing> @0
}`
	file, err := Parse("t.zap", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EmitCPPSingle(file); err == nil {
		t.Error("emitted C++ for a list of an undeclared struct")
	}
}

// TestAStatedWidthIsTheWidth: a record may reserve more than its fields use —
// the X-chain's NFTMintOperation reserves 36 bytes for fields that end at 32 —
// and the emitted size has to be what the wire reserves, not where the fields
// happen to stop.
func TestAStatedWidthIsTheWidth(t *testing.T) {
	file, err := Parse("t.zap", []byte(`package p
struct Wide @36 {
    A u64 @0
    B u64 @8
}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := structSize(file.Structs[0]); got != 36 {
		t.Errorf("size = %d, want the stated 36", got)
	}
	if _, _, err := EmitCPPSingle(file); err != nil {
		t.Fatal(err)
	}
}

// A stated width narrower than the fields is a contradiction, not a layout.
func TestAStatedWidthCannotCutTheFieldsShort(t *testing.T) {
	file, err := Parse("t.zap", []byte(`package p
struct Short @8 {
    A u64 @0
    B u64 @8
}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EmitCPPSingle(file); err == nil {
		t.Error("emitted a struct whose stated width cuts its last field in half")
	}
}

// TestPackagePathNestsInCppAndNotInGo: one path, rendered by each backend the
// way that language spells a namespace.
func TestPackagePathNestsInCppAndNotInGo(t *testing.T) {
	file, err := Parse("t.zap", []byte(`package lux.xvm.wire
struct S {
    A u64 @0
}`))
	if err != nil {
		t.Fatal(err)
	}
	cpp, _, err := emitSingleString(EmitCPPSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cpp, "namespace lux::xvm::wire {") {
		t.Error("C++ output does not open the nested namespace")
	}
	goSrc, _, err := emitSingleString(EmitSingle, file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSrc, "package wire\n") {
		t.Error("Go output does not take the last segment as the package name")
	}
}

// A field named as its struct is refused, because the accessor a backend
// prints for it is a member named as the type is — which C++ reads as a
// constructor. Caught once, in the front end, rather than once per backend.
func TestAFieldMayNotRepeatItsStructsName(t *testing.T) {
	_, err := Parse("x.zap", []byte("package p\nstruct Hash {\n  Kind u8 @0\n  Hash bytes_fixed[32] @1\n}\n"))
	if err == nil {
		t.Fatal("a field named as its struct was accepted")
	}
	if !strings.Contains(err.Error(), "repeats the struct's name") {
		t.Fatalf("error does not say what is wrong: %v", err)
	}
}

// Two fields of one name would print one accessor twice.
func TestTwoFieldsOfOneNameAreRefused(t *testing.T) {
	_, err := Parse("x.zap", []byte("package p\nstruct S {\n  A u8 @0\n  A u8 @1\n}\n"))
	if err == nil {
		t.Fatal("a duplicate field name was accepted")
	}
	if !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("error does not say what is wrong: %v", err)
	}
}
