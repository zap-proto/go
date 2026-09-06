// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2026, Lux Industries Inc. All rights reserved.

package zap

import "testing"

// TestPointerListRoundTrips writes two payloads of different widths, names
// them from one list, and reads both back. A pointer list exists for exactly
// this: an inline run cannot hold records that are not the same width.
func TestPointerListRoundTrips(t *testing.T) {
	b := NewBuilderV2(256)

	first := b.StartObject(8)
	first.SetUint64(0, 0x1122334455667788)
	firstAt := first.Finish()

	second := b.StartObject(16)
	second.SetUint64(0, 9)
	second.SetBytes(8, []byte("wide"))
	secondAt := second.Finish()

	lb := b.StartList(4)
	lb.AddObjectPtr(firstAt)
	lb.AddObjectPtr(secondAt)
	lb.AddObjectPtr(0) // the null element
	listAt, count := lb.Finish()

	root := b.StartObject(8)
	root.SetList(0, listAt, count)
	root.FinishAsRoot()

	m, err := Parse(b.Finish())
	if err != nil {
		t.Fatal(err)
	}
	l := m.Root().ListStride(0, 4)
	if l.Len() != 3 {
		t.Fatalf("len = %d, want 3", l.Len())
	}
	if got := l.ObjectPtr(0).Uint64(0); got != 0x1122334455667788 {
		t.Errorf("element 0 = %#x", got)
	}
	if got := l.ObjectPtr(1).Uint64(0); got != 9 {
		t.Errorf("element 1 = %d", got)
	}
	if got := string(l.ObjectPtr(1).Bytes(8)); got != "wide" {
		t.Errorf("element 1 tail = %q", got)
	}
	if !l.ObjectPtr(2).IsNull() {
		t.Error("the null element is not null")
	}
	if !l.ObjectPtr(3).IsNull() {
		t.Error("reading past the end answered an object")
	}
}

// TestListStrideRefusesALyingLength is the whole reason a reader wants the
// stride: a length word the payload cannot possibly hold is refused once,
// here, rather than at every element.
func TestListStrideRefusesALyingLength(t *testing.T) {
	b := NewBuilderV2(256)
	lb := b.StartList(8)
	lb.AddUint64(1)
	lb.AddUint64(2)
	at, _ := lb.Finish()

	ob := b.StartObject(8)
	ob.SetList(0, at, 30) // two elements on the wire, thirty claimed
	ob.FinishAsRoot()

	m, err := Parse(b.Finish())
	if err != nil {
		t.Fatal(err)
	}
	if l := m.Root().ListStride(0, 8); !l.IsNull() {
		t.Fatalf("stride-8 read accepted a length of %d", l.Len())
	}
	if l := m.Root().List(0); l.Len() != 30 {
		t.Fatalf("the strideless read is meant to be the permissive one, got %d", l.Len())
	}
}
