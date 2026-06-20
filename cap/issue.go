// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import (
	"errors"
	"time"
)

// Issuance describes the request to mint a new root capability.
type Issuance struct {
	Kind        uint32
	Target      [32]byte
	Holder      [32]byte
	Permissions uint64
	Parent      [32]byte // zero = root
	IssuedAt    int64    // 0 = use time.Now()
	ExpiresAt   int64    // 0 = no expiry
	Caveats     []Caveat
}

// errMissingSigner is returned when Issue/Attenuate are called without a
// concrete Signer. We don't ship a default — every cap must be signed.
var errMissingSigner = errors.New("cap: signer required")

// Issue mints a new root capability signed by signer. The signer's
// Public() becomes the cap's Issuer field. Parent stays as supplied
// (zero for a true root; non-zero for re-issuing under an existing
// parent at the cost of caller asserting the chain).
//
// To derive a child cap from an existing parent, use Attenuate.
func Issue(in Issuance, signer Signer) (Cap, error) {
	if signer == nil {
		return Cap{}, errMissingSigner
	}
	if in.IssuedAt == 0 {
		in.IssuedAt = time.Now().Unix()
	}
	issuer := signer.Public()
	raw, err := buildCapBytes(in, issuer, signer)
	if err != nil {
		return Cap{}, err
	}
	return Wrap(raw)
}

// Attenuate derives a child cap from parent by intersecting permissions
// and adding caveats. The child's Issuer = parent's Holder; signer must
// hold the parent's holder key (this is the basis for chain validation:
// each link is signed by the previous holder's key).
//
// The child target equals the parent's target — attenuation never
// broadens scope. permissions is intersected with the parent's
// permissions; passing 0xFFFF... is equivalent to "inherit all".
// caveats are appended (not replaced); chain validation evaluates them
// in conjunction with the parent's.
//
// expiresAt of 0 inherits the parent's expiry; non-zero overrides
// downward (the child cannot outlive the parent).
func Attenuate(parent Cap, holder [32]byte, permissions uint64, caveats []Caveat,
	expiresAt int64, signer Signer) (Cap, error) {
	if signer == nil {
		return Cap{}, errMissingSigner
	}
	if signer.Public() != parent.Holder() {
		// The signer must be the parent's holder; only the holder can
		// delegate authority downward. Anything else would let a
		// stranger mint children, defeating the chain's whole purpose.
		return Cap{}, ErrChainBroken
	}
	// SPEC §7: refuse to build a cap that would fail its own verifier.
	// The delegation gate (SPEC §2.3 step 3d) requires the parent to
	// carry PermAttenuate or be a CapKindDelegate cap; mint-time
	// enforcement means a child that VerifyChain would reject is never
	// produced in the first place.
	if parent.Permissions()&PermAttenuate == 0 && CapKind(parent.Kind()) != KindDelegate {
		return Cap{}, ErrNotDelegable
	}
	parentExpiry := parent.ExpiresAt()
	switch {
	case expiresAt == 0:
		expiresAt = int64(parentExpiry)
	case parentExpiry != 0 && uint64(expiresAt) > parentExpiry:
		expiresAt = int64(parentExpiry)
	}
	parentID := parent.ID()
	in := Issuance{
		Kind:        parent.Kind(),
		Target:      parent.Target(),
		Holder:      holder,
		Permissions: permissions & parent.Permissions(),
		Parent:      parentID,
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   expiresAt,
		Caveats:     caveats,
	}
	issuer := signer.Public()
	raw, err := buildCapBytes(in, issuer, signer)
	if err != nil {
		return Cap{}, err
	}
	return Wrap(raw)
}

// buildCapBytes serializes a capability into the canonical ZAP wire
// format and signs it. The signed payload is SPEC §3 canonical bytes
// (Capability[0..164) || canonical(Caveats)), computed via
// Cap.CanonicalBytes so signer and verifier share one definition. After
// signing, the SigSize-byte signature is patched into the Sig field
// in-place. Returns the final wire bytes.
func buildCapBytes(in Issuance, issuer [32]byte, signer Signer) ([]byte, error) {
	// Build each Caveat as its own ZAP-framed sub-message; the canonical
	// list element is the full ZAP buffer length-prefixed by ListBuilder
	// .AddObjectBytes.
	caveatBufs := make([][]byte, len(in.Caveats))
	for i, cv := range in.Caveats {
		caveatBufs[i] = NewCaveatView(CaveatViewInput{
			Kind:  uint32(cv.Kind),
			Value: cv.Value,
		})
	}

	// First pass: build with Sig = zero. The Sig field's bytes are NOT in
	// the signing scope (CanonicalBytes covers [0..164) + caveats only),
	// so the zero placeholder does not affect the signature.
	raw := NewCapabilityView(CapabilityViewInput{
		Kind:        in.Kind,
		Target:      in.Target,
		Holder:      in.Holder,
		Issuer:      issuer,
		Permissions: in.Permissions,
		Parent:      in.Parent,
		IssuedAt:    uint64(in.IssuedAt),
		ExpiresAt:   uint64(in.ExpiresAt),
		Caveats:     caveatBufs,
		// Sig left as the zero [SigSize]byte.
	})

	// Compute the canonical signing bytes from the just-built buffer using
	// the same code path the verifier uses — no asymmetry between build
	// and verify (SPEC §7).
	c, err := Wrap(raw)
	if err != nil {
		return nil, err
	}
	sig, err := signer.Sign(c.CanonicalBytes())
	if err != nil {
		return nil, err
	}

	// Patch the sig field in-place. The Sig field sits at
	// rootOff + capabilityViewSigOff in the final buffer.
	sigOff := sigAbsOff(raw)
	copy(raw[sigOff:sigOff+SigSize], sig[:])
	return raw, nil
}
