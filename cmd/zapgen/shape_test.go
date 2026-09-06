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
