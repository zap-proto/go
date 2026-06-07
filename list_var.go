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
