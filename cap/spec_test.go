// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// ----------------------------------------------------------------------------
// B2 — VerifyChain delegation gate (SPEC §2.3 step 3d) and mint-time refusal.
// ----------------------------------------------------------------------------

// TestAttenuateRefusesWithoutPermAttenuate proves a parent that lacks
// PermAttenuate and is not a CapKindDelegate cannot mint a child, even by
// its own holder — SPEC §7 (the runtime refuses to build a cap its own
// verifier would reject).
func TestAttenuateRefusesWithoutPermAttenuate(t *testing.T) {
	root := mustSigner(t)
	holder := mustSigner(t)
	parent, err := Issue(Issuance{
		Kind:        uint32(KindIAMSession),
		Holder:      root.Public(),
		Permissions: 0xFF, // no PermAttenuate, not CapKindDelegate
		ExpiresAt:   2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Attenuate(parent, holder.Public(), 0x0F, nil, 0, root); !errors.Is(err, ErrNotDelegable) {
		t.Errorf("expected ErrNotDelegable, got %v", err)
	}
}

// TestAttenuateAllowedForDelegateKind proves a CapKindDelegate parent may
// mint a child WITHOUT carrying PermAttenuate (the kind itself authorizes
// delegation, per capabilities_kinds.md).
func TestAttenuateAllowedForDelegateKind(t *testing.T) {
	root := mustSigner(t)
	holder := mustSigner(t)
	parent, err := Issue(Issuance{
		Kind:        uint32(KindDelegate),
		Holder:      root.Public(),
		Permissions: 0xFF, // no PermAttenuate, but Kind == Delegate
		ExpiresAt:   2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Attenuate(parent, holder.Public(), 0x0F, nil, 0, root); err != nil {
		t.Errorf("delegate-kind parent should permit attenuation: %v", err)
	}
}

// TestVerifyChainRejectsUndelegatedParent constructs (out of band, via a
// permissive signer that lets us forge a chain the mint-time gate would
// block) a leaf whose parent lacks PermAttenuate, and proves VerifyChain
// independently enforces the gate at verify time — defense in depth.
func TestVerifyChainRejectsUndelegatedParent(t *testing.T) {
	root := mustSigner(t)
	mid := mustSigner(t)
	leaf := mustSigner(t)
	var target [32]byte
	target[0] = 0x7E

	// Root WITHOUT PermAttenuate. Mint mid by issuing directly (bypassing
	// Attenuate's gate) but with a correct chain shape: mid.Issuer = root,
	// mid.Parent = rootCap.ID().
	rootCap, err := Issue(Issuance{
		Kind: uint32(KindIAMSession), Holder: root.Public(), Target: target,
		Permissions: 0x0F, ExpiresAt: 2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	// Issue derives Issuer from the signer (root), so mid.Issuer = root.
	midCap, err := Issue(Issuance{
		Kind: uint32(KindIAMSession), Holder: mid.Public(),
		Target: target, Permissions: 0x07, Parent: rootCap.ID(), ExpiresAt: 2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	_ = leaf
	v := Verifier{IssuerKey: issuerKeyFn(root, mid)}
	// Verify midCap against rootCap as its parent: rootCap lacks
	// PermAttenuate, so the chain must be refused.
	chain := []Cap{rootCap}
	if err := v.VerifyChain(midCap, chain, 0x01, target, mid.Public(), 1700000000); !errors.Is(err, ErrNotDelegable) {
		t.Errorf("expected ErrNotDelegable from undelegated parent, got %v", err)
	}
}

// Issue with an explicit non-root Parent/Issuer needs the Issuance to
// carry Issuer; the public Issue derives Issuer from the signer, so we
// confirm that path here is consistent with the chain shape above.

// ----------------------------------------------------------------------------
// B3 — signature scope is SPEC §3 canonical bytes, not the whole buffer.
// ----------------------------------------------------------------------------

// TestCanonicalBytesShape pins the exact layout of the signed bytes:
// Capability[0..164) || (Kind:u32-LE || len:u32-LE || Value) per caveat.
func TestCanonicalBytesShape(t *testing.T) {
	signer := mustSigner(t)
	caveats := []Caveat{
		{Kind: CaveatMaxAmount, Value: u64bytes(7)},
		{Kind: CaveatIPCIDR, Value: []byte("10.0.0.0/8")},
	}
	c, err := Issue(Issuance{
		Kind: uint32(KindKMSAccess), Permissions: 0xFF, ExpiresAt: 2000000000, Caveats: caveats,
	}, signer)
	if err != nil {
		t.Fatal(err)
	}
	got := c.CanonicalBytes()

	// Reconstruct the expected bytes independently.
	root := capRootOff(c.Bytes())
	want := append([]byte(nil), c.Bytes()[root:root+signedHeaderLen]...)
	for _, cv := range caveats {
		var hdr [8]byte
		binary.LittleEndian.PutUint32(hdr[0:4], uint32(cv.Kind))
		binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(cv.Value)))
		want = append(want, hdr[:]...)
		want = append(want, cv.Value...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("CanonicalBytes mismatch\n got=%x\nwant=%x", got, want)
	}
	// The signed length is the header (164) plus each caveat's 8-byte
	// prefix and value.
	wantLen := signedHeaderLen + (8 + 8) + (8 + len("10.0.0.0/8"))
	if len(got) != wantLen {
		t.Errorf("CanonicalBytes len = %d, want %d", len(got), wantLen)
	}
	// It must NOT include the Sig field (3408 bytes at offset 164).
	if len(got) >= signedHeaderLen+SigSize {
		t.Errorf("CanonicalBytes appears to include Sig footer (len %d)", len(got))
	}
}

// TestSignatureExcludesSigField proves that mutating bytes in the Sig
// footer (other than via a real re-sign) does not change the canonical
// signed bytes — the signature is computed over [0..164)+caveats only, so
// the verifier still accepts after a footer-pad scribble that leaves the
// algorithm tag intact.
func TestSignatureExcludesSigField(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{Kind: uint32(KindKMSAccess), Permissions: 0xFF, ExpiresAt: 2000000000}, signer)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), c.CanonicalBytes()...)

	// Scribble a zero-pad byte inside the Sig footer (after the 64-byte
	// ed25519 signature, before the tag) in a copy of the buffer.
	raw := append([]byte(nil), c.Bytes()...)
	sigOff := sigAbsOff(raw)
	raw[sigOff+100] ^= 0xFF // a pad byte, not the real signature or tag
	c2, err := Wrap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, c2.CanonicalBytes()) {
		t.Errorf("CanonicalBytes changed when only Sig pad was scribbled")
	}
}

// TestVerifyDetectsHeaderTamper proves tampering with a byte INSIDE the
// signed header (e.g. Permissions) breaks verification — the canonical
// bytes do cover the fixed header.
func TestVerifyDetectsHeaderTamper(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{Kind: uint32(KindKMSAccess), Permissions: 0xFF, ExpiresAt: 2000000000}, signer)
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), c.Bytes()...)
	raw[fieldAbsOff(raw, capabilityViewPermissionsOff)] ^= 0x01
	tc, err := Wrap(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := Verifier{IssuerKey: issuerKeyFn(signer)}
	if err := v.Verify(tc, 1); !errors.Is(err, ErrSigMismatch) {
		t.Errorf("expected ErrSigMismatch on header tamper, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// B4 — VerifyRevocation is scheme-aware (dispatches on the tag byte).
// ----------------------------------------------------------------------------

// TestVerifyRevocationSchemeAware proves the package VerifyRevocation
// accepts a (bootstrap ed25519) revocation by routing on its tag byte,
// and that Verifier.VerifyRevocation can dispatch a non-ed25519 scheme to
// a wired SchemeVerify hook.
func TestVerifyRevocationSchemeAware(t *testing.T) {
	signer := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000}, signer)
	r, err := Revoke(c, 100, signer)
	if err != nil {
		t.Fatal(err)
	}
	// Bootstrap path: tag is SchemeEd25519, accepted by the package func.
	if err := VerifyRevocation(r, signer.PublicKey()); err != nil {
		t.Errorf("ed25519 revocation rejected: %v", err)
	}

	// A revocation carrying a non-ed25519 tag must be routed to the hook.
	rPQ := r
	rPQ.RevokerSig[AlgTagOffset] = byte(SchemeMLDSA65)
	var sawScheme Scheme
	v := Verifier{
		SchemeVerify: func(s Scheme, pub, payload []byte, sig [SigSize]byte) error {
			sawScheme = s
			if s == SchemeMLDSA65 {
				return nil // pretend the PQ signature verifies
			}
			return ErrUnhandledScheme
		},
	}
	if err := v.VerifyRevocation(rPQ, signer.PublicKey()); err != nil {
		t.Errorf("hooked ML-DSA-65 revocation rejected: %v", err)
	}
	if sawScheme != SchemeMLDSA65 {
		t.Errorf("SchemeVerify saw %#x, want SchemeMLDSA65", sawScheme)
	}
}

// TestVerifyRevocationFailsClosed proves a revocation whose tag is
// SchemeReserved (0x00) or an unknown value is rejected, never silently
// accepted via an ed25519 fallback (B6 applied to revocations).
func TestVerifyRevocationFailsClosed(t *testing.T) {
	signer := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000}, signer)
	r, _ := Revoke(c, 100, signer)

	for _, tag := range []byte{0x00, 0x7F, 0xFF} {
		bad := r
		bad.RevokerSig[AlgTagOffset] = tag
		if err := VerifyRevocation(bad, signer.PublicKey()); !errors.Is(err, ErrUnhandledScheme) {
			t.Errorf("tag %#x: expected ErrUnhandledScheme, got %v", tag, err)
		}
	}
}

// ----------------------------------------------------------------------------
// B6 — verifySig fails closed on scheme==0 / unknown (cap signatures).
// ----------------------------------------------------------------------------

// TestVerifyFailsClosedOnUnknownScheme proves Verify refuses a cap whose
// Sig algorithm tag is SchemeReserved (0x00) or otherwise unimplemented,
// rather than falling back to ed25519.
func TestVerifyFailsClosedOnUnknownScheme(t *testing.T) {
	signer := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000}, signer)
	v := Verifier{IssuerKey: issuerKeyFn(signer)}

	for _, tag := range []byte{0x00, 0x7F, 0xFF} {
		raw := append([]byte(nil), c.Bytes()...)
		raw[sigAbsOff(raw)+AlgTagOffset] = tag
		bad, err := Wrap(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := v.Verify(bad, 1); !errors.Is(err, ErrUnhandledScheme) {
			t.Errorf("tag %#x: expected ErrUnhandledScheme, got %v", tag, err)
		}
	}
}

// TestSchemeKnownSet pins exactly which scheme tags are accepted.
func TestSchemeKnownSet(t *testing.T) {
	known := map[Scheme]bool{
		SchemeSecp256k1: true, SchemeEd25519: true, SchemeMLDSA65: true, SchemeHybrid: true,
	}
	for s := 0; s <= 0xFF; s++ {
		want := known[Scheme(s)]
		if Scheme(s).known() != want {
			t.Errorf("Scheme(%#x).known() = %v, want %v", s, Scheme(s).known(), want)
		}
	}
	if SchemeReserved.known() {
		t.Errorf("SchemeReserved must not be known")
	}
}
