// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import "encoding/binary"

// signedHeaderLen is the length of the fixed-header prefix that the
// signature covers: Capability bytes [0..164), i.e. Kind through the
// Caveats list pointer, NOT including Sig. Equal to capabilityViewSigOff
// (Sig begins exactly where the signed header ends).
const signedHeaderLen = capabilityViewSigOff

// CanonicalBytes returns the exact bytes a Capability's signature is
// computed over, per SPEC.md §3 "Signature chain":
//
//	Capability[0..164)  ||  for each Caveat in list order:
//	    Kind:u32-LE || len(Value):u32-LE || Value
//
// The fixed-header prefix is read verbatim from the wire buffer (so the
// signer and verifier agree on the on-wire field bytes), but the caveat
// section is RECOMPUTED from the decoded caveats rather than copied from
// the ZAP heap. This is deliberate: it excludes the heap-area pointer
// indirection bytes, so a tamperer cannot perturb the signature by
// rewriting heap layout, and it makes the signed bytes identical across
// language runtimes regardless of how each lays out its heap.
//
// CanonicalBytes does NOT include Sig (bytes [164..3572)); that is the
// field being authenticated.
func (c Cap) CanonicalBytes() []byte {
	hdrOff := capRootOff(c.raw)
	// Fixed header [0..164).
	out := make([]byte, 0, signedHeaderLen+caveatsCanonicalLen(c.Caveats()))
	out = append(out, c.raw[hdrOff:hdrOff+signedHeaderLen]...)
	// Canonical caveat encoding, in list order.
	for _, cv := range c.Caveats() {
		out = appendCaveatCanonical(out, cv)
	}
	return out
}

// appendCaveatCanonical appends one caveat's canonical encoding (Kind:u32-LE
// || len(Value):u32-LE || Value) to dst and returns the extended slice.
func appendCaveatCanonical(dst []byte, cv Caveat) []byte {
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(cv.Kind))
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(cv.Value)))
	dst = append(dst, hdr[:]...)
	dst = append(dst, cv.Value...)
	return dst
}

// caveatsCanonicalLen returns the byte length the canonical caveat
// section will occupy, used to size the CanonicalBytes buffer up front.
func caveatsCanonicalLen(cvs []Caveat) int {
	n := 0
	for _, cv := range cvs {
		n += 8 + len(cv.Value)
	}
	return n
}

// capRootOff returns the absolute offset of the root object (the start of
// the Capability fixed header) within a wire buffer. The root offset is
// stored in the ZAP header at bytes [8:12].
func capRootOff(raw []byte) int {
	return int(binary.LittleEndian.Uint32(raw[8:12]))
}
