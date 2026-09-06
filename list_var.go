// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import "encoding/binary"

// SetBytesFixed copies len(v) bytes inline at fieldOffset within the
// object's fixed payload. Used for ids, signatures, public keys —
// anything declared as bytes_fixed[N] in a .zap schema. Symmetric to
// Object.BytesFixed on the read side.
//
// A zero-length argument is a no-op (the slot retains the zero value).
func (ob *ObjectBuilder) SetBytesFixed(fieldOffset int, v []byte) {
	if len(v) == 0 {
		return
	}
	ob.ensureField(fieldOffset + len(v))
	copy(ob.b.buf[ob.startPos+fieldOffset:], v)
}

// AddObjectBytes appends a single variable-length entry to a list:
// 4-byte little-endian length prefix followed by data. Increments the
// element count by 1 (in contrast with AddBytes, which appends raw
// bytes to a flat byte-stream list and increments count by len(data)).
//
// Used by codegen-emitted builders for `list<T>` fields where T is a
// nested struct or a variable-width bytes/text payload. The matching
// reader is List.ObjectAt / List.BytesAt.
func (lb *ListBuilder) AddObjectBytes(data []byte) {
	lb.b.grow(4 + len(data))
	binary.LittleEndian.PutUint32(lb.b.buf[lb.b.pos:], uint32(len(data)))
	lb.b.pos += 4
	copy(lb.b.buf[lb.b.pos:], data)
	lb.b.pos += len(data)
	lb.count++
}

// FinishOffset returns just the list's start offset. Used by codegen-
// emitted builders that track the element count externally (the count
// is then passed to ObjectBuilder.SetList alongside the offset).
//
// The parallel runtime's primary Finish() returns (offset, length),
// suited for in-flight count tracking. FinishOffset is the single-value
// counterpart for the codegen pattern.
func (lb *ListBuilder) FinishOffset() int {
	return lb.startPos
}

// ObjectPtr returns element i of a list of POINTERS: a 4-byte signed offset
// from the element's own position, dereferenced exactly as Object.Object
// does. The objects lie in the same buffer, written before the pointer run,
// so the offsets are usually negative.
func (l List) ObjectPtr(i int) Object {
	if i < 0 || i >= l.length {
		return Object{}
	}
	d := l.msg.data
	pos := l.offset + i*4
	if pos+4 > len(d) {
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

// AddObjectPtr appends one 4-byte SIGNED pointer to the object at target.
// The element kind of a list whose members live elsewhere in the buffer:
// they are written first and the pointer run after, so the offsets are
// usually negative. Zero writes the null pointer.
func (lb *ListBuilder) AddObjectPtr(target int) {
	lb.b.grow(4)
	p := lb.b.pos
	v := uint32(0)
	if target != 0 {
		v = uint32(int32(target - p))
	}
	binary.LittleEndian.PutUint32(lb.b.buf[p:], v)
	lb.b.pos = p + 4
	lb.count++
}

// SetRoot names the object at offset as what the message is about. A builder
// that writes one object names it with ObjectBuilder.FinishAsRoot; one that
// writes several says here which of them the reader starts from.
func (b *Builder) SetRoot(offset int) {
	b.rootOffset = offset
}
