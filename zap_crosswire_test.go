// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Cross-wire conformance: this runtime (zap-proto/go) and the hardened
// downstream runtime (github.com/luxfi/zap) implement the SAME wire format.
// This test pins that contract from THIS side without importing luxfi/zap
// (which would drag its post-quantum / QUIC / mDNS dependency tree into the
// pure-stdlib baseline). The proof has two halves that meet at a shared
// golden vector:
//
//   - zap-proto/go side (here): build the canonical object with THIS
//     Builder, assert the bytes equal goldenV1Hex, and assert this reader
//     decodes every field. Also assert this reader now ACCEPTS a v2-header
//     buffer and REJECTS the adversarial backward-pointer buffer.
//   - luxfi/zap side (zap_crosswire_test.go in that repo): build the SAME
//     object with luxfi's Builder (via NewBuilderV1) and assert the bytes
//     equal the SAME goldenV1Hex.
//
// Because both repos pin the identical constant, byte-for-byte wire identity
// is proven across the two runtimes with zero cross-module dependency. The
// live "encode here, decode there" equivalence was additionally verified in a
// throwaway go.work harness joining both modules (see the consolidation
// report); this committed test is the durable regression guard.
//
// Canonical object layout (dataSize = 24), exercising the full mechanism —
// a fixed scalar plus two variable-length tail pointers (text + bytes):
//
//	field @0  uint32 = 0xDEADBEEF   (fixed scalar)
//	field @8  text   = "zap"        (8-byte slot {relOffset,length}, tail-patched)
//	field @16 bytes  = 01 02 03 04  (8-byte slot {relOffset,length}, tail-patched)
const (
	xwU32   = 0
	xwText  = 8
	xwBytes = 16
	xwSize  = 24

	// goldenV1Hex is the canonical 47-byte ZAP buffer (Version1) for the
	// layout above. SHARED VERBATIM with luxfi/zap's cross-wire test. Changing
	// it on one side without the other is a wire break and MUST fail CI.
	//
	//   5a415000               magic "ZAP\0"
	//   0100                   version 1
	//   0000                   flags 0
	//   10000000               root offset = 16
	//   2f000000               size = 47
	//   efbeadde               uint32 @ root+0  = 0xDEADBEEF
	//   00000000               pad   @ root+4   (8-align the text slot)
	//   10000000 03000000      text slot @ root+8:  relOffset 16, length 3
	//   0b000000 04000000      bytes slot @ root+16: relOffset 11, length 4
	//   7a6170                 "zap"            text tail @ 40
	//   01020304               bytes tail @ 43
	goldenV1Hex = "5a41500001000000100000002f000000efbeadde0000000010000000030000000b000000040000007a617001020304"
)

// buildCanonical builds the canonical object with THIS runtime's Builder.
func buildCanonical(tb testing.TB) []byte {
	tb.Helper()
	b := NewBuilder(64)
	ob := b.StartObject(xwSize)
	ob.SetUint32(xwU32, 0xDEADBEEF)
	ob.SetText(xwText, "zap")
	ob.SetBytes(xwBytes, []byte{0x01, 0x02, 0x03, 0x04})
	ob.FinishAsRoot()
	return b.Finish()
}

// TestCrossWireGoldenEncode proves THIS Builder emits exactly the shared
// golden vector. Version is the bare-constant default for this runtime
// (Version1) — so the full buffer must match goldenV1Hex byte-for-byte.
func TestCrossWireGoldenEncode(t *testing.T) {
	if Version != Version1 {
		t.Fatalf("this runtime's default Version = %d, want Version1 (%d); golden vector assumes a v1 header", Version, Version1)
	}
	got := buildCanonical(t)
	want, err := hex.DecodeString(goldenV1Hex)
	if err != nil {
		t.Fatalf("decode goldenV1Hex: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical encode mismatch:\n got  %s\n want %s", hex.EncodeToString(got), goldenV1Hex)
	}
}

// TestCrossWireGoldenDecode proves THIS reader decodes the shared golden
// vector field-by-field. This is the "decode with the other" leg: the bytes
// are the canonical wire (identical to luxfi/zap's NewBuilderV1 output).
func TestCrossWireGoldenDecode(t *testing.T) {
	buf, err := hex.DecodeString(goldenV1Hex)
	if err != nil {
		t.Fatalf("decode goldenV1Hex: %v", err)
	}
	msg, err := Parse(buf)
	if err != nil {
		t.Fatalf("Parse(golden) failed: %v", err)
	}
	if msg.Version() != Version1 {
		t.Fatalf("golden Version = %d, want %d", msg.Version(), Version1)
	}
	r := msg.Root()
	if got := r.Uint32(xwU32); got != 0xDEADBEEF {
		t.Errorf("Uint32 = %#x, want 0xDEADBEEF", got)
	}
	if got := r.Text(xwText); got != "zap" {
		t.Errorf("Text = %q, want %q", got, "zap")
	}
	if got := r.Bytes(xwBytes); !bytes.Equal(got, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Errorf("Bytes = %v, want [1 2 3 4]", got)
	}
}

// TestCrossWireAcceptsV2 proves this reader now ACCEPTS a Version2-header
// buffer. The v2 buffer is the golden v1 buffer with byte 4 flipped to 2 —
// the data segment is otherwise identical (the documented header delta), so
// every field must still decode. This is the change that lets a buffer
// emitted by luxfi/zap's default NewBuilder (which tags Version2) be parsed
// by this runtime; before the consolidation it returned ErrInvalidVersion.
func TestCrossWireAcceptsV2(t *testing.T) {
	v1, err := hex.DecodeString(goldenV1Hex)
	if err != nil {
		t.Fatalf("decode goldenV1Hex: %v", err)
	}
	v2 := append([]byte(nil), v1...)
	v2[4] = byte(Version2) // flip version 1 -> 2

	msg, err := Parse(v2)
	if err != nil {
		t.Fatalf("Parse(v2) failed: %v (reader must accept Version2)", err)
	}
	if msg.Version() != Version2 {
		t.Fatalf("v2 Version = %d, want %d", msg.Version(), Version2)
	}
	r := msg.Root()
	if got := r.Uint32(xwU32); got != 0xDEADBEEF {
		t.Errorf("v2 Uint32 = %#x, want 0xDEADBEEF", got)
	}
	if got := r.Text(xwText); got != "zap" {
		t.Errorf("v2 Text = %q, want %q", got, "zap")
	}
	if got := r.Bytes(xwBytes); !bytes.Equal(got, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Errorf("v2 Bytes = %v, want [1 2 3 4]", got)
	}

	// And the data segment past magic+version is identical between v1 and v2.
	if !bytes.Equal(v1[6:], v2[6:]) {
		t.Fatal("v1 and v2 data segments diverge past the version byte")
	}
}

// TestCrossWireRejectsBackwardPointer proves the ported hardening: a crafted
// Bytes pointer aiming BACKWARD into the wire header is rejected (returns
// empty) instead of aliasing header bytes. Before the hardening the signed
// int32 cast followed the pointer and leaked the version field. luxfi/zap
// already rejected this; the two readers now agree on accept/reject for this
// adversarial input.
func TestCrossWireRejectsBackwardPointer(t *testing.T) {
	// [16B header][root object @16: one bytes slot @ field 0]; total 24.
	buf := make([]byte, 24)
	copy(buf[0:4], Magic)
	putU16(buf[4:6], Version1)
	putU16(buf[6:8], 0)
	putU32(buf[8:12], 16) // root offset
	putU32(buf[12:16], 24) // size
	// bytes field at absolute pos 16; relOffset = -12 lands absPos at 4
	// (inside the header). int32(-12) bit pattern = 0xFFFFFFF4.
	rel := int32(-12)
	putU32(buf[16:20], uint32(rel))
	putU32(buf[20:24], 2) // length

	msg, err := Parse(buf) // header itself is well-formed -> Parse succeeds
	if err != nil {
		t.Fatalf("Parse failed unexpectedly: %v", err)
	}
	if got := msg.Root().Bytes(0); got != nil {
		t.Fatalf("backward header-aliasing pointer must yield nil, got %v", got)
	}
}

// Small endian helpers kept local to the test (the package's binary import is
// only used in non-test code; tests avoid widening the package surface).
func putU16(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
