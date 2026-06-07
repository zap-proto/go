// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cap implements the ZAP capability runtime.
//
// A Capability is a signed, attenuable token of authority. It grants a
// holder permission to perform a bitmask of operations on a target, with
// optional caveats. Caps form a chain: a parent's holder can issue an
// attenuated child cap whose permissions are a subset of the parent's.
// VerifyChain walks the chain back to a root, checking each signature,
// the intersection of permissions, expiry, revocation, and caveats.
//
// The wire format is a fixed-offset prefix followed by a length-prefixed
// caveat block and a fixed-size trailing signature, so field reads are
// O(1) and zero-allocation. Cap.Bytes() returns the raw buffer without
// copying.
//
//	┌───────────────────────────────────────────┐ offset
//	│ Kind        uint32                        │   0
//	│ Target      [32]byte                      │   4
//	│ Holder      [32]byte                      │  36
//	│ Issuer      [32]byte                      │  68
//	│ Permissions uint64                        │ 100
//	│ Parent      [32]byte                      │ 108
//	│ IssuedAt    uint64                        │ 140
//	│ ExpiresAt   uint64                        │ 148
//	│ Magic       [4]byte = "ZCAP"              │ 156
//	│ NumCaveats  uint32                        │ 160
//	│ CaveatsLen  uint32                        │ 164
//	│ Caveats     bytes (CaveatsLen)            │ 168
//	│ Signature   [96]byte                      │ end - 96
//	└───────────────────────────────────────────┘
//
// Each caveat in the caveats block is encoded as:
//
//	uint32 Kind  | uint32 ValueLen | bytes Value
//
// The signature covers Bytes()[0 : len(Bytes())-96]. The cap ID is the
// hash of the full Bytes() including signature; revocation lists key on
// ID.
package cap

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// Layout constants. Field offsets within the fixed prefix.
const (
	offKind        = 0
	offTarget      = 4
	offHolder      = 36
	offIssuer      = 68
	offPermissions = 100
	offParent      = 108
	offIssuedAt    = 140
	offExpiresAt   = 148
	offMagic       = 156
	offNumCaveats  = 160
	offCaveatsLen  = 164
	offCaveats     = 168

	// PrefixSize is the byte length of the fixed prefix (before caveats).
	PrefixSize = 168

	// SigSize is the fixed signature footer size (ML-DSA-65 placeholder;
	// real ML-DSA-65 sigs are larger and would extend this constant).
	SigSize = 96
)

// Magic identifies a ZAP capability buffer. Distinct from the ZAP
// message magic ("ZAP\x00") because a cap is a self-contained blob
// addressable by hash, not a length-framed ZAP message.
const Magic = "ZCAP"

// CapKind enumerates the kinds of authority a capability can confer.
type CapKind uint32

const (
	KindReserved   CapKind = 0x00
	KindIAMSession CapKind = 0x01
	KindIAMAPIKey  CapKind = 0x02
	KindKMSAccess  CapKind = 0x10
	KindKMSSign    CapKind = 0x11
	KindMPCSign    CapKind = 0x20
	KindATSOrder   CapKind = 0x30
	KindBridgeXfer CapKind = 0x40
	KindStake      CapKind = 0x50
	KindDelegate   CapKind = 0xFF
)

// CaveatKind enumerates the kinds of caveat that can be attached.
type CaveatKind uint32

const (
	CaveatExpiresAt CaveatKind = 0x00
	CaveatMaxAmount CaveatKind = 0x01
	CaveatDestChain CaveatKind = 0x02
	CaveatRateLimit CaveatKind = 0x03
	CaveatIPCIDR    CaveatKind = 0x04
	CaveatAssetID   CaveatKind = 0x05
	CaveatOpAllow   CaveatKind = 0x06
	CaveatMaxDepth  CaveatKind = 0x07
	CaveatAudience  CaveatKind = 0x08
	CaveatNonceHash CaveatKind = 0x09
)

// Caveat is one constraint attached to a capability.
type Caveat struct {
	Kind  CaveatKind
	Value []byte
}

// Errors returned by Wrap and chain validation.
var (
	ErrTooShort        = errors.New("cap: buffer too short")
	ErrBadMagic        = errors.New("cap: bad magic")
	ErrBadCaveats      = errors.New("cap: caveat block malformed")
	ErrSigMismatch     = errors.New("cap: signature does not verify")
	ErrExpired         = errors.New("cap: expired")
	ErrRevoked         = errors.New("cap: revoked")
	ErrChainBroken     = errors.New("cap: chain link broken")
	ErrPermsExceedPar  = errors.New("cap: permissions exceed parent")
	ErrOpNotPermitted  = errors.New("cap: op not in permission mask")
	ErrTargetMismatch  = errors.New("cap: target does not match")
	ErrHolderMismatch  = errors.New("cap: holder does not match")
	ErrIssuerUnknown   = errors.New("cap: issuer key unknown")
	ErrCaveatViolation = errors.New("cap: caveat violated")
)

// Cap is a zero-copy view over a capability buffer. Constructed by Wrap.
// All accessors read directly from raw without allocating.
type Cap struct {
	raw []byte
}

// Wrap parses a capability buffer and returns a typed zero-copy view.
// Validates only the framing — magic + prefix size + caveat block bounds
// + signature trailer. Cryptographic verification lives in Verifier.
func Wrap(b []byte) (Cap, error) {
	if len(b) < PrefixSize+SigSize {
		return Cap{}, ErrTooShort
	}
	if string(b[offMagic:offMagic+4]) != Magic {
		return Cap{}, ErrBadMagic
	}
	caveatsLen := binary.LittleEndian.Uint32(b[offCaveatsLen:])
	if PrefixSize+int(caveatsLen)+SigSize > len(b) {
		return Cap{}, ErrBadCaveats
	}
	if PrefixSize+int(caveatsLen)+SigSize != len(b) {
		return Cap{}, ErrBadCaveats
	}
	return Cap{raw: b}, nil
}

// Bytes returns the underlying buffer without copying.
func (c Cap) Bytes() []byte { return c.raw }

// Kind reads the kind field at offset 0.
func (c Cap) Kind() uint32 {
	return binary.LittleEndian.Uint32(c.raw[offKind:])
}

// Target reads the 32-byte target hash at offset 4.
func (c Cap) Target() [32]byte {
	var out [32]byte
	copy(out[:], c.raw[offTarget:offTarget+32])
	return out
}

// Holder reads the 32-byte holder hash at offset 36.
func (c Cap) Holder() [32]byte {
	var out [32]byte
	copy(out[:], c.raw[offHolder:offHolder+32])
	return out
}

// Issuer reads the 32-byte issuer hash at offset 68.
func (c Cap) Issuer() [32]byte {
	var out [32]byte
	copy(out[:], c.raw[offIssuer:offIssuer+32])
	return out
}

// Permissions reads the permission bitmask at offset 100.
func (c Cap) Permissions() uint64 {
	return binary.LittleEndian.Uint64(c.raw[offPermissions:])
}

// Parent reads the 32-byte parent cap ID at offset 108. Zero means root.
func (c Cap) Parent() [32]byte {
	var out [32]byte
	copy(out[:], c.raw[offParent:offParent+32])
	return out
}

// IssuedAt reads the unix-second issued-at timestamp at offset 140.
func (c Cap) IssuedAt() uint64 {
	return binary.LittleEndian.Uint64(c.raw[offIssuedAt:])
}

// ExpiresAt reads the unix-second expiry at offset 148. Zero means never.
func (c Cap) ExpiresAt() uint64 {
	return binary.LittleEndian.Uint64(c.raw[offExpiresAt:])
}

// NumCaveats returns the caveat count from the prefix header.
func (c Cap) NumCaveats() int {
	return int(binary.LittleEndian.Uint32(c.raw[offNumCaveats:]))
}

// caveatsLen returns the byte length of the caveat block.
func (c Cap) caveatsLen() int {
	return int(binary.LittleEndian.Uint32(c.raw[offCaveatsLen:]))
}

// CaveatAt returns the i-th caveat. Out-of-range returns a zero Caveat.
// The Value slice aliases the underlying buffer; callers must not mutate it.
// Walks the caveat block from the start each call: O(i). For hot paths
// iterate via Caveats().
func (c Cap) CaveatAt(i int) Caveat {
	if i < 0 || i >= c.NumCaveats() {
		return Caveat{}
	}
	p := offCaveats
	end := offCaveats + c.caveatsLen()
	for k := 0; k < i; k++ {
		if p+8 > end {
			return Caveat{}
		}
		vlen := int(binary.LittleEndian.Uint32(c.raw[p+4:]))
		p += 8 + vlen
	}
	if p+8 > end {
		return Caveat{}
	}
	kind := CaveatKind(binary.LittleEndian.Uint32(c.raw[p:]))
	vlen := int(binary.LittleEndian.Uint32(c.raw[p+4:]))
	if p+8+vlen > end {
		return Caveat{}
	}
	return Caveat{Kind: kind, Value: c.raw[p+8 : p+8+vlen]}
}

// Caveats returns the slice of caveats decoded in one walk. Values alias
// the buffer; do not mutate. Allocates the result slice header only.
func (c Cap) Caveats() []Caveat {
	n := c.NumCaveats()
	if n == 0 {
		return nil
	}
	out := make([]Caveat, 0, n)
	p := offCaveats
	end := offCaveats + c.caveatsLen()
	for k := 0; k < n; k++ {
		if p+8 > end {
			return out
		}
		kind := CaveatKind(binary.LittleEndian.Uint32(c.raw[p:]))
		vlen := int(binary.LittleEndian.Uint32(c.raw[p+4:]))
		if p+8+vlen > end {
			return out
		}
		out = append(out, Caveat{Kind: kind, Value: c.raw[p+8 : p+8+vlen]})
		p += 8 + vlen
	}
	return out
}

// Signature returns the 96-byte trailing signature.
func (c Cap) Signature() [SigSize]byte {
	var out [SigSize]byte
	copy(out[:], c.raw[len(c.raw)-SigSize:])
	return out
}

// SignedBytes returns the slice that was hashed and signed: the cap
// buffer minus the trailing signature. Zero-copy.
func (c Cap) SignedBytes() []byte {
	return c.raw[:len(c.raw)-SigSize]
}

// ID returns the canonical 32-byte identifier of this cap: SHA-256 of
// the full buffer (including signature). Revocation records key on ID.
//
// BLAKE3 swap-point: replace sha256.Sum256 with blake3.Sum256 in v1.1
// once luxfi/crypto exposes a stable BLAKE3 entry point. Both are
// 256-bit; consumers should treat the ID as opaque bytes.
func (c Cap) ID() [32]byte {
	return sha256.Sum256(c.raw)
}

// Hash32 is the package's canonical 32-byte hash function. Exposed so
// signers and verifiers agree on the digest construction. SHA-256 today,
// BLAKE3 in v1.1 (see ID).
func Hash32(b []byte) [32]byte {
	return sha256.Sum256(b)
}
