// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

// Spec-named accessors. These are the symbols generated code calls.
// They sit alongside the runtime's primary accessors and either delegate
// to existing methods (when the wire shape matches) or implement the
// missing piece (BytesFixed on Object).

// BytesFixed returns a zero-copy slice of n inline bytes at fieldOffset
// within the object's fixed payload. Used for ids, signatures, public
// keys — anything declared as bytes_fixed[N] in a .zap schema.
//
// Returns nil if the requested span falls outside the buffer. The
// returned slice aliases the underlying message data; the caller MUST
// NOT mutate it.
func (o Object) BytesFixed(fieldOffset, n int) []byte {
	if n <= 0 {
		return nil
	}
	pos := o.offset + fieldOffset
	if pos < 0 || pos+n > len(o.msg.data) {
		return nil
	}
	return o.msg.data[pos : pos+n]
}

// Length returns the list's wire-encoded element count. Spec-named alias
// for Len().
func (l List) Length() int { return l.length }

// Uint32At returns the i-th uint32 element. Spec-named alias for Uint32.
func (l List) Uint32At(i int) uint32 { return l.Uint32(i) }

// Uint64At returns the i-th uint64 element. Spec-named alias for Uint64.
func (l List) Uint64At(i int) uint64 { return l.Uint64(i) }

// ObjectAt returns the i-th element of a variable-element list as an
// Object. Each element on the wire is a self-contained ZAP buffer
// preceded by a 4-byte little-endian length prefix (the shape emitted
// by ListBuilder.AddBytes for elemSize=0).
//
// Returns Object{} if the index is out of range or the sub-buffer fails
// to parse.
func (l List) ObjectAt(i int) Object {
	if i < 0 || i >= l.length || l.msg == nil {
		return Object{}
	}
	p := l.offset
	data := l.msg.data
	for k := 0; k < i; k++ {
		if p+4 > len(data) {
			return Object{}
		}
		sz := readU32(data, p)
		p += 4 + int(sz)
	}
	if p+4 > len(data) {
		return Object{}
	}
	sz := readU32(data, p)
	start := p + 4
	end := start + int(sz)
	if end > len(data) {
		return Object{}
	}
	sub, err := Parse(data[start:end])
	if err != nil {
		return Object{}
	}
	return sub.Root()
}

// BytesAt returns the i-th raw-bytes element of a variable-element list.
// Element layout matches ListBuilder.AddBytes for elemSize=0: a 4-byte
// length prefix followed by the entry bytes.
func (l List) BytesAt(i int) []byte {
	if i < 0 || i >= l.length || l.msg == nil {
		return nil
	}
	p := l.offset
	data := l.msg.data
	for k := 0; k < i; k++ {
		if p+4 > len(data) {
			return nil
		}
		sz := readU32(data, p)
		p += 4 + int(sz)
	}
	if p+4 > len(data) {
		return nil
	}
	sz := readU32(data, p)
	start := p + 4
	end := start + int(sz)
	if end > len(data) {
		return nil
	}
	return data[start:end]
}

func readU32(b []byte, off int) uint32 {
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}
