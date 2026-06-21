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
// Wire format is canonical ZAP — magic+version header, object data,
// length-prefixed list of Caveat sub-messages. The bytes are produced
// and read via the generated zero-copy views in capabilities_zap.go.
// This file is the thin idiomatic-Go wrapper that the IAM/KMS/MPC
// consumers see: Wrap / Cap.Field / Verifier.
//
// Signature scope: per SPEC.md §3, the signed bytes are the canonical
// concatenation Capability[0..164) || canonical(Caveats) — the fixed
// header up to and including the Caveats list pointer, followed by each
// Caveat encoded as Kind:u32-LE || len(Value):u32-LE || Value in list
// order. This EXCLUDES the Sig field itself and the ZAP heap-area
// indirection bytes, and is recomputed identically by signer and
// verifier (see canonical.go, Cap.CanonicalBytes), so heap layout cannot
// be tampered with without breaking the signature and the signed bytes
// are identical across every language runtime.
package cap

import (
	"crypto/sha256"
	"errors"

	zap "github.com/zap-proto/go"
)

// SigSize is the fixed signature footer width in bytes. Sized at v1.1 to
// hold any of:
//
//   - secp256k1 ECDSA   (65 bytes)
//   - Ed25519           (64 bytes)
//   - ML-DSA-65         (3309 bytes, FIPS 204 §5.2 Level-3)
//   - hybrid Ed25519+ML-DSA-65 (3373 bytes + 16-byte separator)
//
// 3408 is the smallest 16-byte-aligned size that fits FIPS 204 ML-DSA-65
// (3309 B) with a 99-byte headroom. Schemes shorter than SigSize are
// zero-padded on the right; verifiers identify the scheme via the
// algorithm tag in the final byte (Sig[SigSize-1]) and decode the
// leading L_scheme bytes accordingly. See zap-spec/capabilities.zap and
// capabilities_kinds.md for the canonical tag table.
const SigSize = 3408

// AlgTagOffset is the offset of the algorithm-tag byte within the SigSize
// signature footer. The byte at [SigSize-1] identifies which signature
// primitive a verifier MUST use; it is part of the signed payload, so a
// tag flip changes the signature and is caught by verifier mismatch.
const AlgTagOffset = SigSize - 1

// Scheme is the wire-level signature algorithm tag. The numeric values
// MUST match capabilities_kinds.md "Wire schemes" table; the byte written
// at Sig[AlgTagOffset] is one of these constants.
//
// Verifiers fail-closed on SchemeReserved and on values not enumerated
// here. A consumer's auth layer may re-export a typed alias whose
// constants reuse these numeric values one-for-one so the wire byte and
// the typed enum agree.
type Scheme uint8

const (
	// SchemeReserved (0x00) MUST NOT appear in valid caps. Verifiers
	// reject it so a zero-filled / never-initialised footer is caught.
	SchemeReserved Scheme = 0x00
	// SchemeSecp256k1 is 65-byte secp256k1 ECDSA (R||S||v).
	SchemeSecp256k1 Scheme = 0x01
	// SchemeEd25519 is 64-byte Ed25519 (RFC 8032).
	SchemeEd25519 Scheme = 0x02
	// SchemeMLDSA65 is the 3309-byte FIPS 204 Level-3 ML-DSA-65 signature.
	SchemeMLDSA65 Scheme = 0x03
	// SchemeHybrid is concatenated Ed25519 || ML-DSA-65 (64 + 3309 = 3373 B).
	SchemeHybrid Scheme = 0x04
)

// known reports whether s is one of the registered signature schemes a
// verifier may accept. Per SPEC §2.3 step 3c the valid set is exactly
// {0x01,0x02,0x03,0x04}; SchemeReserved (0x00) and any unassigned tag
// are NOT known, so verifiers fail-closed on them.
func (s Scheme) known() bool {
	switch s {
	case SchemeSecp256k1, SchemeEd25519, SchemeMLDSA65, SchemeHybrid:
		return true
	default:
		return false
	}
}

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

// Caveat is one constraint attached to a capability. Value bytes alias
// the underlying ZAP buffer when produced via Cap.CaveatAt / Cap.Caveats;
// callers must not mutate Value in-place. Caveat literals passed into
// Issue/Attenuate are copied during build.
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
	ErrNotDelegable    = errors.New("cap: parent does not permit attenuation")
	ErrOpNotPermitted  = errors.New("cap: op not in permission mask")
	ErrTargetMismatch  = errors.New("cap: target does not match")
	ErrHolderMismatch  = errors.New("cap: holder does not match")
	ErrIssuerUnknown   = errors.New("cap: issuer key unknown")
	ErrCaveatViolation = errors.New("cap: caveat violated")
	// ErrUnhandledScheme means the algorithm-tag byte is one the verifier
	// does not implement (or SchemeReserved / an unknown tag). It is both
	// the value a SchemeVerify hook returns to decline a tag (so the
	// dispatcher may try its built-in ed25519 bootstrap path for
	// SchemeEd25519) AND the terminal error the dispatcher returns when no
	// path handles the tag — fail-closed per SPEC §2.3 step 3c.
	ErrUnhandledScheme = errors.New("cap: signature scheme not handled")
)

// Cap is a zero-copy view over a capability buffer. Constructed by Wrap.
// All accessors read directly from raw without allocating; Value slices
// returned from CaveatAt / Caveats alias the underlying buffer.
type Cap struct {
	raw  []byte
	view CapabilityView
}

// Wrap parses a capability buffer and returns a typed zero-copy view.
// Validates ZAP framing (magic, version, size) plus capability-specific
// structural checks (sig field within bounds). Cryptographic verification
// lives in Verifier.
func Wrap(b []byte) (Cap, error) {
	if len(b) < zap.HeaderSize {
		return Cap{}, ErrTooShort
	}
	view, err := WrapCapabilityView(b)
	if err != nil {
		// Distinguish "bad magic" from generic short-buffer to keep the
		// historic error surface stable.
		if errors.Is(err, zap.ErrInvalidMagic) {
			return Cap{}, ErrBadMagic
		}
		if errors.Is(err, zap.ErrBufferTooSmall) {
			return Cap{}, ErrTooShort
		}
		return Cap{}, err
	}
	// Sanity-check that the sig field is within bounds: the sig must
	// occupy SigSize bytes inside the buffer.
	if sigAbsOff(b)+SigSize > len(b) {
		return Cap{}, ErrTooShort
	}
	c := Cap{raw: b, view: view}
	// Walk caveats once to catch bad framing up front; expensive paths
	// (Verify) re-walk via the view, so this is a cheap eager check.
	list := view.Caveats()
	for i := 0; i < list.Length(); i++ {
		if list.BytesAt(i) == nil && list.Length() > 0 {
			return Cap{}, ErrBadCaveats
		}
	}
	return c, nil
}

// sigAbsOff returns the absolute byte offset of the Sig field in raw.
// The Sig sits at capabilityViewSigOff bytes after the root-object's
// absolute start. The root-object offset is stored in the ZAP header at
// bytes [8:12].
func sigAbsOff(raw []byte) int {
	return capRootOff(raw) + capabilityViewSigOff
}

// Bytes returns the underlying buffer without copying.
func (c Cap) Bytes() []byte { return c.raw }

// Kind reads the kind field.
func (c Cap) Kind() uint32 { return c.view.Kind() }

// Target reads the 32-byte target hash.
func (c Cap) Target() [32]byte { return c.view.Target() }

// Holder reads the 32-byte holder hash.
func (c Cap) Holder() [32]byte { return c.view.Holder() }

// Issuer reads the 32-byte issuer hash.
func (c Cap) Issuer() [32]byte { return c.view.Issuer() }

// Permissions reads the permission bitmask.
func (c Cap) Permissions() uint64 { return c.view.Permissions() }

// Parent reads the 32-byte parent cap ID. Zero means root.
func (c Cap) Parent() [32]byte { return c.view.Parent() }

// IssuedAt reads the unix-second issued-at timestamp.
func (c Cap) IssuedAt() uint64 { return c.view.IssuedAt() }

// ExpiresAt reads the unix-second expiry. Zero means never.
func (c Cap) ExpiresAt() uint64 { return c.view.ExpiresAt() }

// NumCaveats returns the number of caveats attached to this cap.
func (c Cap) NumCaveats() int { return c.view.Caveats().Length() }

// CaveatAt returns the i-th caveat. Out-of-range returns a zero Caveat.
// The Value slice aliases the underlying buffer; callers must not mutate
// it. Each call re-walks the variable-element list: O(i).
func (c Cap) CaveatAt(i int) Caveat {
	list := c.view.Caveats()
	if i < 0 || i >= list.Length() {
		return Caveat{}
	}
	sub := list.ObjectAt(i)
	if sub.IsNull() {
		return Caveat{}
	}
	return Caveat{
		Kind:  CaveatKind(sub.Uint32(caveatViewKindOff)),
		Value: sub.Bytes(caveatViewValueOff),
	}
}

// Caveats returns the slice of caveats decoded in one walk. Values alias
// the buffer; do not mutate.
func (c Cap) Caveats() []Caveat {
	list := c.view.Caveats()
	n := list.Length()
	if n == 0 {
		return nil
	}
	out := make([]Caveat, 0, n)
	for i := 0; i < n; i++ {
		sub := list.ObjectAt(i)
		if sub.IsNull() {
			return out
		}
		out = append(out, Caveat{
			Kind:  CaveatKind(sub.Uint32(caveatViewKindOff)),
			Value: sub.Bytes(caveatViewValueOff),
		})
	}
	return out
}

// Signature returns the SigSize-byte signature stored in the Sig field.
func (c Cap) Signature() [SigSize]byte { return c.view.Sig() }

// ID returns the canonical 32-byte identifier of this cap. Per SPEC.md §4
// the CapID is Hash32(CanonicalBytes(cap) || Sig) — the exact bytes
// signed at issue time plus the signature footer. Revocation records key
// on ID, and the chain walk matches each child's Parent to its parent's
// ID, so this construction is what binds the chain.
func (c Cap) ID() [32]byte {
	sig := c.view.Sig()
	buf := c.CanonicalBytes()
	buf = append(buf, sig[:]...)
	return Hash32(buf)
}

// Hash32 is the package's canonical 32-byte hash function. Exposed so
// signers and verifiers agree on the digest construction. SHA-256 is the
// spec-mandated CapID hash (SPEC.md §4): in every target language's stdlib,
// so cross-language CapIDs are trivially reproducible and the runtime stays
// zero-dependency. Treat the result as an opaque content identifier.
func Hash32(b []byte) [32]byte {
	return sha256.Sum256(b)
}
