// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

// Single-pass cursors for variable-element lists.
//
// A variable-element list (the shape ListBuilder.AddObjectBytes emits) is
// a stream of entries, each a 4-byte little-endian length prefix followed
// by that many payload bytes:
//
//	[len0][payload0][len1][payload1] … [lenN-1][payloadN-1]
//
// The indexed accessors List.BytesAt(i) / List.ObjectAt(i) locate element
// i by re-walking the stream from offset 0 every call (each is O(i)), so
// decoding all N elements by index is O(N²). For an attacker-supplied
// frame that is an algorithmic-complexity DoS: N cheap real elements
// within the frame cap burn O(N²) CPU.
//
// EachBytes / Each walk the stream ONCE — p advances by 4+len per entry,
// one pass total = O(N). They are the iteration primitive; BytesAt /
// ObjectAt remain for random access and are unchanged.
//
// Bounds discipline (identical to BytesAt/ObjectAt, just carried forward
// in p instead of restarted):
//   - stop before reading a length prefix that would run off the buffer
//     (p+4 > len(data));
//   - stop before yielding a payload that would run off the buffer
//     (start+len > len(data));
//   - never yield more than List.length entries.
//
// Because every entry occupies at least its 4-byte prefix, a list living
// in a buffer of B bytes can hold at most B/4 entries; the walk therefore
// self-bounds at B/4 regardless of a forged length word — no count
// ceiling argument is needed for the cursor to be DoS-safe. A truncated
// or over-claiming stream simply stops at the first entry that does not
// fit, exactly where BytesAt(i) would return nil/zero. Whether a yielded
// entry's bytes are themselves well-formed is the caller's concern.

// EachBytes walks a variable-element list once, calling fn with the index
// and the zero-copy bytes of each entry, in order. It is the single-pass
// counterpart to calling BytesAt(0)…BytesAt(n-1): identical elements, but
// O(N) total instead of O(N²).
//
// fn returns true to continue, false to stop early (the caller's break).
// The walk also stops when the declared element count is exhausted or the
// next entry would read past the buffer — the latter is the same
// "stream exhausted" signal BytesAt conveys by returning nil, so a short
// or over-claiming stream yields only its real entries and never
// fabricates one.
//
// An empty-payload entry (4-byte zero-length prefix) yields a non-nil
// zero-length slice, matching BytesAt — a genuine "" survives. The
// returned slices alias the underlying message data; the caller MUST NOT
// mutate them.
func (l List) EachBytes(fn func(i int, b []byte) bool) {
	if l.msg == nil {
		return
	}
	data := l.msg.data
	p := l.offset
	for i := 0; i < l.length; i++ {
		if p+4 > len(data) {
			return // header runs off the buffer: stream exhausted
		}
		sz := int(readU32(data, p))
		start := p + 4
		end := start + sz
		if sz < 0 || end < start || end > len(data) {
			return // payload runs off the buffer (or length overflows): exhausted
		}
		if !fn(i, data[start:end]) {
			return
		}
		p = end
	}
}

// Each walks a variable-element list once, parsing each entry's bytes into
// an Object and calling fn with the index and that Object, in order. It is
// the single-pass counterpart to calling ObjectAt(0)…ObjectAt(n-1):
// identical elements, but O(N) total instead of O(N²).
//
// An entry whose bytes do not parse as a ZAP message yields the zero
// Object (Object{}, IsNull() == true) — the same value ObjectAt returns
// for an unparseable element — while the cursor still advances by the
// entry's wire length, so iteration position stays correct. fn returns
// true to continue, false to stop early; returning false on IsNull
// reproduces ObjectAt's "stop at the first absent/garbage element"
// semantics. The walk also stops when the element count is exhausted or
// the next entry would read past the buffer.
func (l List) Each(fn func(i int, elem Object) bool) {
	l.EachBytes(func(i int, b []byte) bool {
		var elem Object
		if sub, err := Parse(b); err == nil {
			elem = sub.Root()
		}
		return fn(i, elem)
	})
}
