// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import "encoding/binary"

// The pointer list, and the strided read of any list.
//
// A list of records that are all the same width is those records back to
// back, and List.Object(i, stride) already reads one. A list of records that
// are NOT all the same width cannot be laid out that way, because the reader
// has no way to find the i'th; the wire answers with a run of 4-byte signed
// relative pointers, each naming a payload written elsewhere in the same
// message. That is the shape here, and it is the same shape the C++ runtime
// spells add_object_ptr / object_ptr.

// ptrElem is the width of one element of a pointer list.
const ptrElem = 4

// AddObjectPtr appends one 4-byte SIGNED relative pointer to an object
// already written at target, and counts ONE element. A target of 0 is a null
// element.
//
// Signed, because a builder finishes a child before its parent, so the
// payload usually sits EARLIER in the buffer than the pointer naming it and
// the offset runs backwards.
func (lb *ListBuilder) AddObjectPtr(target int) {
	lb.b.grow(ptrElem)
	if target == 0 {
		binary.LittleEndian.PutUint32(lb.b.buf[lb.b.pos:], 0)
	} else {
		binary.LittleEndian.PutUint32(lb.b.buf[lb.b.pos:], uint32(int32(target-lb.b.pos)))
	}
	lb.b.pos += ptrElem
	lb.count++
}

// ObjectPtr returns element i of a pointer list, dereferenced exactly as
// Object.Object dereferences a nested-object field: a target inside the wire
// header is refused, and so is one past the end.
func (l List) ObjectPtr(i int) Object {
	if i < 0 || i >= l.length || l.msg == nil {
		return Object{}
	}
	d := l.msg.data
	pos := l.offset + i*ptrElem
	if pos < 0 || pos+ptrElem > len(d) {
		return Object{}
	}
	rel := int32(binary.LittleEndian.Uint32(d[pos:]))
	if rel == 0 {
		return Object{}
	}
	abs := pos + int(rel)
	if abs < HeaderSize || abs >= len(d) {
		return Object{}
	}
	return Object{msg: l.msg, offset: abs}
}

// ListStride is List with the tighter clamp a schema makes possible: length
// times stride has to fit what is left of the buffer, so a lying length word
// is refused once, here, instead of at every element accessor.
//
// List is this with stride 0 — the permissive bound, for a caller that does
// not know the width.
func (o Object) ListStride(fieldOffset int, stride uint32) List {
	l := o.List(fieldOffset)
	if l.msg == nil || stride == 0 {
		return l
	}
	remaining := uint64(len(o.data()) - l.offset)
	if uint64(l.length)*uint64(stride) > remaining {
		return List{}
	}
	return l
}
