// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zap implements the Zero-copy Application Protocol (ZAP) runtime.
//
// ZAP is a binary serialization format designed for high-performance
// inter-process and network communication. Like Cap'n Proto and FlatBuffers,
// ZAP enables zero-copy reads - data can be accessed directly from the
// underlying byte buffer without parsing or allocation.
//
// This package provides the canonical Go runtime for the ZAP wire format.
// The specification lives at github.com/zap-proto/zap-spec; code generation
// from ZAP schemas is handled by github.com/zap-proto/go/cmd/zapgen (not
// yet released in this skeleton).
//
// Wire Format:
//
//	┌─────────────────────────────────────────────────┐
//	│ Header (16 bytes)                               │
//	│  ├─ Magic (4 bytes): "ZAP\x00"                  │
//	│  ├─ Version (2 bytes): 1 or 2                   │
//	│  ├─ Flags (2 bytes): compression, etc.          │
//	│  ├─ Root Offset (4 bytes): offset to root       │
//	│  └─ Size (4 bytes): total message size          │
//	├─────────────────────────────────────────────────┤
//	│ Data Segment (variable)                         │
//	│  └─ Structs, lists, text, bytes...              │
//	└─────────────────────────────────────────────────┘
//
// All multi-byte integers are little-endian. Offsets are relative to
// the position of the offset field itself.
package zap

import (
	"encoding/binary"
	"errors"
	"math"
	"unsafe"
)

const (
	// HeaderSize is the size of the ZAP message header
	HeaderSize = 16

	// Magic bytes identifying a ZAP message
	Magic = "ZAP\x00"

	// ZAP wire versions. Two schema generations share the same data-segment
	// encoding and differ only in the meaning of the leading struct byte:
	//
	//   Version1 — original layout (e.g. legacy platformvm v2 schema, where
	//              byte 0 of the root struct is a payload field).
	//   Version2 — adds a one-byte discriminator at struct byte 0 (the v3
	//              platformvm TxKind tag); every later field shifts by +1.
	//
	// The reader ACCEPTS both versions (forward-compatible parse); consumers
	// that require a specific schema generation gate on Message.Version after
	// Parse. The data segment past the 6-byte magic+version prefix is byte-
	// identical regardless of version — the version byte is the only header
	// difference between a v1 and a v2 buffer carrying the same payload.
	//
	// Version (the bare constant) is the version this runtime's Builder emits
	// by default. zap-proto/go is the pure-stdlib baseline runtime and emits
	// Version1; the hardened downstream runtime (github.com/luxfi/zap) emits
	// Version2 by default. Both readers accept both — see the cross-wire
	// conformance test (zap_crosswire_test.go).
	Version1 uint16 = 1
	Version2 uint16 = 2
	Version  uint16 = Version1

	// DefaultPort is the canonical TCP port for ZAP transport. Like 80
	// means HTTP and 443 means HTTPS, 9999 means ZAP — services that
	// host ZAP endpoints bind this port by convention; the DNS name
	// disambiguates which service is on the other end.
	DefaultPort = 9999

	// Alignment for data segments
	Alignment = 8
)

// Flags for message header
const (
	FlagNone       uint16 = 0
	FlagCompressed uint16 = 1 << 0
	FlagEncrypted  uint16 = 1 << 1
	FlagSigned     uint16 = 1 << 2
)

var (
	ErrInvalidMagic   = errors.New("zap: invalid magic bytes")
	ErrInvalidVersion = errors.New("zap: unsupported version")
	ErrBufferTooSmall = errors.New("zap: buffer too small")
	ErrOutOfBounds    = errors.New("zap: offset out of bounds")
	ErrInvalidOffset  = errors.New("zap: invalid offset")
)

// Message is a ZAP message that can be read zero-copy.
type Message struct {
	data []byte
}

// Parse parses a ZAP message from bytes without copying.
//
// Accepts both Version1 and Version2 wire headers (forward-compatible read).
// Callers that require a specific schema generation gate on Message.Version
// after Parse.
//
// The declared size field must be at least HeaderSize: a size=0 buffer would
// otherwise pass Parse and then panic on subsequent Root()/Flags() reads
// against the empty slice. It is rejected at the wire boundary.
func Parse(data []byte) (*Message, error) {
	if len(data) < HeaderSize {
		return nil, ErrBufferTooSmall
	}

	// Check magic
	if string(data[0:4]) != Magic {
		return nil, ErrInvalidMagic
	}

	// Check version (accept v1 + v2; reject anything else).
	version := binary.LittleEndian.Uint16(data[4:6])
	if version != Version1 && version != Version2 {
		return nil, ErrInvalidVersion
	}

	// Validate size: must be at least the header (else Root()/Flags() would
	// panic on data[:size]) and at most the input length.
	size := binary.LittleEndian.Uint32(data[12:16])
	if int(size) < HeaderSize || int(size) > len(data) {
		return nil, ErrBufferTooSmall
	}

	return &Message{data: data[:size]}, nil
}

// Version returns the wire version of the message (Version1 or Version2).
func (m *Message) Version() uint16 {
	return binary.LittleEndian.Uint16(m.data[4:6])
}

// Bytes returns the underlying byte slice.
func (m *Message) Bytes() []byte {
	return m.data
}

// Size returns the total message size.
func (m *Message) Size() int {
	return len(m.data)
}

// Flags returns the message flags.
func (m *Message) Flags() uint16 {
	return binary.LittleEndian.Uint16(m.data[6:8])
}

// Root returns the root object of the message.
func (m *Message) Root() Object {
	offset := binary.LittleEndian.Uint32(m.data[8:12])
	return Object{msg: m, offset: int(offset)}
}

// Object is a zero-copy view into a ZAP struct.
type Object struct {
	msg    *Message
	offset int
}

// IsNull returns true if the object is null.
func (o Object) IsNull() bool {
	return o.offset == 0
}

// Bool reads a bool at the given field offset.
func (o Object) Bool(fieldOffset int) bool {
	return o.Uint8(fieldOffset) != 0
}

// Uint8 reads a uint8 at the given field offset.
func (o Object) Uint8(fieldOffset int) uint8 {
	pos := o.offset + fieldOffset
	if pos >= len(o.msg.data) {
		return 0
	}
	return o.msg.data[pos]
}

// Uint16 reads a uint16 at the given field offset.
func (o Object) Uint16(fieldOffset int) uint16 {
	pos := o.offset + fieldOffset
	if pos+2 > len(o.msg.data) {
		return 0
	}
	return binary.LittleEndian.Uint16(o.msg.data[pos:])
}

// Uint32 reads a uint32 at the given field offset.
func (o Object) Uint32(fieldOffset int) uint32 {
	pos := o.offset + fieldOffset
	if pos+4 > len(o.msg.data) {
		return 0
	}
	return binary.LittleEndian.Uint32(o.msg.data[pos:])
}

// Uint64 reads a uint64 at the given field offset.
func (o Object) Uint64(fieldOffset int) uint64 {
	pos := o.offset + fieldOffset
	if pos+8 > len(o.msg.data) {
		return 0
	}
	return binary.LittleEndian.Uint64(o.msg.data[pos:])
}

// Int8 reads an int8 at the given field offset.
func (o Object) Int8(fieldOffset int) int8 {
	return int8(o.Uint8(fieldOffset))
}

// Int16 reads an int16 at the given field offset.
func (o Object) Int16(fieldOffset int) int16 {
	return int16(o.Uint16(fieldOffset))
}

// Int32 reads an int32 at the given field offset.
func (o Object) Int32(fieldOffset int) int32 {
	return int32(o.Uint32(fieldOffset))
}

// Int64 reads an int64 at the given field offset.
func (o Object) Int64(fieldOffset int) int64 {
	return int64(o.Uint64(fieldOffset))
}

// Float32 reads a float32 at the given field offset.
func (o Object) Float32(fieldOffset int) float32 {
	return math.Float32frombits(o.Uint32(fieldOffset))
}

// Float64 reads a float64 at the given field offset.
func (o Object) Float64(fieldOffset int) float64 {
	return math.Float64frombits(o.Uint64(fieldOffset))
}

// Text reads a string at the given field offset (zero-copy).
func (o Object) Text(fieldOffset int) string {
	b := o.Bytes(fieldOffset)
	if len(b) == 0 {
		return ""
	}
	// Zero-copy string conversion
	return unsafe.String(&b[0], len(b))
}

// Bytes reads a byte slice at the given field offset (zero-copy).
//
// Wire-format rule: relOffset is an UNSIGNED forward pointer from the field
// position into the variable section. Negative bit-patterns (high bit set)
// flow through uint32→int conversion as large positive values and are
// rejected by the absPos+length > len(data) bounds check. This closes the
// pointer-escape malleability surface where a signed cast would let a crafted
// relOffset alias bytes back inside the fixed section or the wire header — a
// Bytes target can never legitimately live in offsets 0..HeaderSize-1.
func (o Object) Bytes(fieldOffset int) []byte {
	pos := o.offset + fieldOffset
	if pos+4 > len(o.msg.data) {
		return nil
	}

	// Read offset (relative, unsigned forward pointer) and length.
	relOffset := binary.LittleEndian.Uint32(o.msg.data[pos:])
	if relOffset == 0 {
		return nil // Null
	}

	lenPos := pos + 4
	if lenPos+4 > len(o.msg.data) {
		return nil
	}
	length := binary.LittleEndian.Uint32(o.msg.data[lenPos:])

	// Calculate absolute position. Reject any payload that lands inside the
	// wire header — Bytes targets cannot live in offsets 0..HeaderSize-1.
	absPos := pos + int(relOffset)
	if absPos < HeaderSize {
		return nil
	}
	if absPos+int(length) > len(o.msg.data) {
		return nil
	}

	return o.msg.data[absPos : absPos+int(length)]
}

// Object reads a nested object at the given field offset.
//
// Wire-format rule: relOffset is SIGNED. The builder may finalize a nested
// object BEFORE its parent (in which case the nested payload lives EARLIER in
// the variable section than the parent's pointer cell, and the relOffset is
// negative). The bounds check rejects any absOffset outside the message; for
// the Bytes-malleability rule see Bytes().
//
// An attacker can use a backward relOffset to alias the WIRE HEADER (offsets
// 0..HeaderSize-1). The header carries Magic/Version/Flags/RootOffset/Size —
// none of which is a legitimate object payload. Any absOffset < HeaderSize is
// rejected. The signed cast still lets honest builders point backward to
// nested objects they finalized first (which live at offset >= HeaderSize).
func (o Object) Object(fieldOffset int) Object {
	pos := o.offset + fieldOffset
	if pos+4 > len(o.msg.data) {
		return Object{}
	}

	relOffset := int32(binary.LittleEndian.Uint32(o.msg.data[pos:]))
	if relOffset == 0 {
		return Object{} // Null
	}

	absOffset := pos + int(relOffset)
	if absOffset < HeaderSize || absOffset >= len(o.msg.data) {
		return Object{}
	}

	return Object{msg: o.msg, offset: absOffset}
}

// List reads a list at the given field offset.
//
// Wire-format rule: relOffset is SIGNED (see Object()). Any absOffset <
// HeaderSize is rejected (lists cannot start inside the wire header). The
// length field is bounded by the total message size — an attacker-set
// length=0xFFFFFFFF would otherwise let a downstream `for i := 0; i < Len()`
// loop iterate 4G times even though every per-element accessor returns 0.
func (o Object) List(fieldOffset int) List {
	pos := o.offset + fieldOffset
	if pos+8 > len(o.msg.data) {
		return List{}
	}

	relOffset := int32(binary.LittleEndian.Uint32(o.msg.data[pos:]))
	if relOffset == 0 {
		return List{} // Null
	}

	length := binary.LittleEndian.Uint32(o.msg.data[pos+4:])

	// Clamp length to the message size. The tightest bound is
	// length*minElementSize, but element size is per-list-accessor (Uint8 is
	// 1B, Uint32 is 4B, struct lists carry their own stride). The wire layer
	// cannot know the stride, so use the permissive `length <= len(data)`
	// baseline — every per-element accessor re-checks its own bounds. This
	// rejects the 0xFFFFFFFF DoS without false-rejecting honest 1-byte-stride
	// lists that span the entire message.
	if int(length) > len(o.msg.data) {
		return List{}
	}

	absOffset := pos + int(relOffset)
	if absOffset < HeaderSize || absOffset >= len(o.msg.data) {
		return List{}
	}

	return List{msg: o.msg, offset: absOffset, length: int(length)}
}

// List is a zero-copy view into a ZAP list.
type List struct {
	msg    *Message
	offset int
	length int
}

// Len returns the number of elements.
func (l List) Len() int {
	return l.length
}

// IsNull returns true if the list is null.
func (l List) IsNull() bool {
	return l.msg == nil
}

// Uint8 returns a uint8 list element.
func (l List) Uint8(i int) uint8 {
	if i < 0 || i >= l.length {
		return 0
	}
	pos := l.offset + i
	if pos >= len(l.msg.data) {
		return 0
	}
	return l.msg.data[pos]
}

// Uint32 returns a uint32 list element.
func (l List) Uint32(i int) uint32 {
	if i < 0 || i >= l.length {
		return 0
	}
	pos := l.offset + i*4
	if pos+4 > len(l.msg.data) {
		return 0
	}
	return binary.LittleEndian.Uint32(l.msg.data[pos:])
}

// Uint64 returns a uint64 list element.
func (l List) Uint64(i int) uint64 {
	if i < 0 || i >= l.length {
		return 0
	}
	pos := l.offset + i*8
	if pos+8 > len(l.msg.data) {
		return 0
	}
	return binary.LittleEndian.Uint64(l.msg.data[pos:])
}

// Object returns an object list element.
func (l List) Object(i int, elemSize int) Object {
	if i < 0 || i >= l.length {
		return Object{}
	}
	return Object{msg: l.msg, offset: l.offset + i*elemSize}
}

// Bytes returns the raw bytes of the list (for byte lists).
func (l List) Bytes() []byte {
	if l.msg == nil || l.offset+l.length > len(l.msg.data) {
		return nil
	}
	return l.msg.data[l.offset : l.offset+l.length]
}
