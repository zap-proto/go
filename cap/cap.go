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
// Signature scope: the full ZAP buffer with the 96-byte Sig field
// zeroed. Verifier reconstructs the signing payload by zeroing the sig
// field in a local copy and verifying the captured Signature() against
// it. See SPEC.md "Signature scope".
package cap

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	zap "github.com/zap-proto/go"
)

// SigSize is the fixed signature footer size (ML-DSA-65 placeholder;
// real ML-DSA-65 sigs are larger and would extend this constant).
const SigSize = 96

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
	ErrOpNotPermitted  = errors.New("cap: op not in permission mask")
	ErrTargetMismatch  = errors.New("cap: target does not match")
	ErrHolderMismatch  = errors.New("cap: holder does not match")
	ErrIssuerUnknown   = errors.New("cap: issuer key unknown")
	ErrCaveatViolation = errors.New("cap: caveat violated")
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
	root := int(binary.LittleEndian.Uint32(raw[8:12]))
	return root + capabilityViewSigOff
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

// Signature returns the 96-byte signature stored in the Sig field.
func (c Cap) Signature() [SigSize]byte { return c.view.Sig() }

// SignedBytes returns the bytes that were signed when this cap was
// issued: the full ZAP buffer with the 96-byte Sig field replaced by
// zeros. Allocates a copy of len(c.raw) bytes — the underlying buffer
// is not mutated. Used by Verifier and by signers in Issue/Attenuate.
func (c Cap) SignedBytes() []byte {
	out := make([]byte, len(c.raw))
	copy(out, c.raw)
	off := sigAbsOff(out)
	for i := off; i < off+SigSize && i < len(out); i++ {
		out[i] = 0
	}
	return out
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
