// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
)

// issuerKeyFn builds a Verifier.IssuerKey lookup over a known map of
// signers keyed by their Public() hash.
func issuerKeyFn(signers ...*Ed25519Signer) func([32]byte) ([]byte, error) {
	m := make(map[[32]byte]ed25519.PublicKey, len(signers))
	for _, s := range signers {
		m[s.Public()] = s.PublicKey()
	}
	return func(h [32]byte) ([]byte, error) {
		pk, ok := m[h]
		if !ok {
			return nil, ErrIssuerUnknown
		}
		return pk, nil
	}
}

func mustSigner(t *testing.T) *Ed25519Signer {
	t.Helper()
	s, err := NewEd25519Signer()
	if err != nil {
		t.Fatalf("NewEd25519Signer: %v", err)
	}
	return s
}

// ----------------------------------------------------------------------------
// Round-trip: Issue → Bytes → Wrap → field equality.
// ----------------------------------------------------------------------------

func TestIssueRoundTrip(t *testing.T) {
	signer := mustSigner(t)
	var target, holder [32]byte
	for i := range target {
		target[i] = byte(i)
		holder[i] = byte(255 - i)
	}

	in := Issuance{
		Kind:        uint32(KindIAMSession),
		Target:      target,
		Holder:      holder,
		Permissions: 0xDEADBEEFCAFEBABE,
		IssuedAt:    1700000000,
		ExpiresAt:   2000000000,
		Caveats: []Caveat{
			{Kind: CaveatMaxAmount, Value: u64bytes(1_000_000)},
			{Kind: CaveatRateLimit, Value: u32pair(60, 10)},
			{Kind: CaveatIPCIDR, Value: []byte("10.0.0.0/8")},
		},
	}
	c, err := Issue(in, signer)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if c.Kind() != uint32(KindIAMSession) {
		t.Errorf("Kind = %#x, want %#x", c.Kind(), uint32(KindIAMSession))
	}
	if c.Target() != target {
		t.Errorf("Target mismatch")
	}
	if c.Holder() != holder {
		t.Errorf("Holder mismatch")
	}
	if c.Issuer() != signer.Public() {
		t.Errorf("Issuer mismatch")
	}
	if c.Permissions() != 0xDEADBEEFCAFEBABE {
		t.Errorf("Permissions = %#x", c.Permissions())
	}
	if c.IssuedAt() != 1700000000 {
		t.Errorf("IssuedAt = %d", c.IssuedAt())
	}
	if c.ExpiresAt() != 2000000000 {
		t.Errorf("ExpiresAt = %d", c.ExpiresAt())
	}
	if c.NumCaveats() != 3 {
		t.Fatalf("NumCaveats = %d, want 3", c.NumCaveats())
	}

	// Caveat decode round-trip.
	cv0 := c.CaveatAt(0)
	if cv0.Kind != CaveatMaxAmount || !bytes.Equal(cv0.Value, u64bytes(1_000_000)) {
		t.Errorf("Caveat 0 mismatch: %+v", cv0)
	}
	cv1 := c.CaveatAt(1)
	if cv1.Kind != CaveatRateLimit || !bytes.Equal(cv1.Value, u32pair(60, 10)) {
		t.Errorf("Caveat 1 mismatch: %+v", cv1)
	}
	cv2 := c.CaveatAt(2)
	if cv2.Kind != CaveatIPCIDR || string(cv2.Value) != "10.0.0.0/8" {
		t.Errorf("Caveat 2 mismatch: %+v", cv2)
	}

	// Caveats() should agree with CaveatAt.
	all := c.Caveats()
	if len(all) != 3 || all[2].Kind != CaveatIPCIDR {
		t.Errorf("Caveats() mismatch: %+v", all)
	}

	// Bytes() round-trips through Wrap.
	rewrapped, err := Wrap(c.Bytes())
	if err != nil {
		t.Fatalf("re-Wrap: %v", err)
	}
	if rewrapped.Kind() != c.Kind() {
		t.Errorf("re-Wrap field drift")
	}
}

// ----------------------------------------------------------------------------
// Verify accepts a freshly minted cap and rejects expired / revoked /
// wrong-issuer / tampered caps.
// ----------------------------------------------------------------------------

func TestVerifyAcceptsFresh(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{
		Kind:        uint32(KindKMSAccess),
		Permissions: 0xFF,
		ExpiresAt:   2000000000,
	}, signer)
	if err != nil {
		t.Fatal(err)
	}
	v := Verifier{IssuerKey: issuerKeyFn(signer)}
	if err := v.Verify(c, 1700000000); err != nil {
		t.Errorf("Verify rejected fresh cap: %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{
		Kind:        uint32(KindKMSAccess),
		Permissions: 0xFF,
		ExpiresAt:   1700000000,
	}, signer)
	if err != nil {
		t.Fatal(err)
	}
	v := Verifier{IssuerKey: issuerKeyFn(signer)}
	if err := v.Verify(c, 1700000001); !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestVerifyRejectsRevoked(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{Permissions: 1}, signer)
	if err != nil {
		t.Fatal(err)
	}
	v := Verifier{
		IssuerKey: issuerKeyFn(signer),
		IsRevoked: func(id [32]byte) bool { return id == c.ID() },
	}
	if err := v.Verify(c, 1); !errors.Is(err, ErrRevoked) {
		t.Errorf("expected ErrRevoked, got %v", err)
	}
}

func TestVerifyRejectsUnknownIssuer(t *testing.T) {
	signer := mustSigner(t)
	other := mustSigner(t)
	c, err := Issue(Issuance{Permissions: 1}, signer)
	if err != nil {
		t.Fatal(err)
	}
	// Verifier only knows 'other', not the cap's issuer.
	v := Verifier{IssuerKey: issuerKeyFn(other)}
	if err := v.Verify(c, 1); !errors.Is(err, ErrIssuerUnknown) {
		t.Errorf("expected ErrIssuerUnknown, got %v", err)
	}
}

func TestVerifyRejectsTamperedBuffer(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{Permissions: 1}, signer)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a permission bit. The Permissions field sits at
	// rootOff + capabilityViewPermissionsOff in the ZAP buffer.
	tampered := append([]byte(nil), c.Bytes()...)
	tampered[fieldAbsOff(tampered, capabilityViewPermissionsOff)] ^= 0x01
	tc, err := Wrap(tampered)
	if err != nil {
		t.Fatal(err)
	}
	v := Verifier{IssuerKey: issuerKeyFn(signer)}
	if err := v.Verify(tc, 1); !errors.Is(err, ErrSigMismatch) {
		t.Errorf("expected ErrSigMismatch, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Attenuate: child intersects permissions and child's parent = parent.ID().
// ----------------------------------------------------------------------------

func TestAttenuateIntersectsPermissions(t *testing.T) {
	root := mustSigner(t)
	child := mustSigner(t)

	var target [32]byte
	target[0] = 0xAB

	parent, err := Issue(Issuance{
		Kind:        uint32(KindATSOrder),
		Target:      target,
		Holder:      root.Public(),
		Permissions: PermAttenuate | 0b11110000,
		ExpiresAt:   2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := Attenuate(parent, child.Public(), 0b10100110, []Caveat{
		{Kind: CaveatMaxAmount, Value: u64bytes(100)},
	}, 0, root)
	if err != nil {
		t.Fatalf("Attenuate: %v", err)
	}

	if leaf.Permissions() != (0b11110000 & 0b10100110) {
		t.Errorf("Permissions = %#b, want %#b", leaf.Permissions(), 0b11110000&0b10100110)
	}
	if leaf.Parent() != parent.ID() {
		t.Errorf("Parent pointer not set")
	}
	if leaf.Issuer() != root.Public() {
		t.Errorf("Child issuer should equal parent holder")
	}
	if leaf.Target() != target {
		t.Errorf("Target should be inherited")
	}
	if leaf.ExpiresAt() != parent.ExpiresAt() {
		t.Errorf("Expiry should default to parent")
	}
}

func TestAttenuateRequiresParentHolderKey(t *testing.T) {
	root := mustSigner(t)
	imposter := mustSigner(t)
	holder := mustSigner(t)

	parent, err := Issue(Issuance{
		Permissions: 0xFF,
		Holder:      root.Public(),
		ExpiresAt:   2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	// Imposter is not the parent's holder — must refuse.
	if _, err := Attenuate(parent, holder.Public(), 0xFF, nil, 0, imposter); !errors.Is(err, ErrChainBroken) {
		t.Errorf("expected ErrChainBroken, got %v", err)
	}
}

func TestAttenuateCapsExpiryDownward(t *testing.T) {
	root := mustSigner(t)
	holder := mustSigner(t)
	parent, err := Issue(Issuance{
		Permissions: PermAttenuate | 0xFF,
		Holder:      root.Public(),
		ExpiresAt:   1000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := Attenuate(parent, holder.Public(), PermAttenuate|0xFF, nil, 9999, root)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.ExpiresAt() != 1000 {
		t.Errorf("child expiry not clamped to parent: %d", leaf.ExpiresAt())
	}
}

// ----------------------------------------------------------------------------
// VerifyChain: walks parent links, dies on revoked parent.
// ----------------------------------------------------------------------------

func TestVerifyChainHappyPath(t *testing.T) {
	root := mustSigner(t)
	mid := mustSigner(t)
	leaf := mustSigner(t)

	var target [32]byte
	target[31] = 0xEE

	rootCap, err := Issue(Issuance{
		Kind:        uint32(KindMPCSign),
		Target:      target,
		Holder:      root.Public(),
		Permissions: PermAttenuate | 0xFF,
		ExpiresAt:   2000000000,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	// mid keeps PermAttenuate so it may in turn issue leafCap.
	midCap, err := Attenuate(rootCap, mid.Public(), PermAttenuate|0x0F, nil, 0, root)
	if err != nil {
		t.Fatal(err)
	}
	leafCap, err := Attenuate(midCap, leaf.Public(), 0x07, nil, 0, mid)
	if err != nil {
		t.Fatal(err)
	}

	v := Verifier{IssuerKey: issuerKeyFn(root, mid, leaf)}
	chain := []Cap{midCap, rootCap}
	if err := v.VerifyChain(leafCap, chain, 0x04, target, leaf.Public(), 1700000000); err != nil {
		t.Errorf("VerifyChain rejected good chain: %v", err)
	}
}

func TestVerifyChainRejectsRevokedParent(t *testing.T) {
	root := mustSigner(t)
	mid := mustSigner(t)
	leaf := mustSigner(t)

	var target [32]byte
	target[0] = 0x01

	rootCap, _ := Issue(Issuance{Holder: root.Public(), Target: target, Permissions: PermAttenuate | 0xFF, ExpiresAt: 2000000000}, root)
	midCap, _ := Attenuate(rootCap, mid.Public(), PermAttenuate|0x0F, nil, 0, root)
	leafCap, _ := Attenuate(midCap, leaf.Public(), 0x07, nil, 0, mid)

	revoked := midCap.ID()
	v := Verifier{
		IssuerKey: issuerKeyFn(root, mid, leaf),
		IsRevoked: func(id [32]byte) bool { return id == revoked },
	}
	chain := []Cap{midCap, rootCap}
	err := v.VerifyChain(leafCap, chain, 0x04, target, leaf.Public(), 1700000000)
	if !errors.Is(err, ErrRevoked) {
		t.Errorf("expected ErrRevoked, got %v", err)
	}
}

func TestVerifyChainRejectsBrokenLink(t *testing.T) {
	root := mustSigner(t)
	mid := mustSigner(t)
	leaf := mustSigner(t)
	other := mustSigner(t)

	var target [32]byte
	rootCap, _ := Issue(Issuance{Holder: root.Public(), Target: target, Permissions: PermAttenuate | 0xFF, ExpiresAt: 2000000000}, root)
	midCap, _ := Attenuate(rootCap, mid.Public(), PermAttenuate|0x0F, nil, 0, root)
	leafCap, _ := Attenuate(midCap, leaf.Public(), 0x07, nil, 0, mid)
	// Build an unrelated cap; pass it as the "parent" to break the link.
	bogus, _ := Issue(Issuance{Holder: other.Public(), Target: target, Permissions: PermAttenuate | 0xFF, ExpiresAt: 2000000000}, other)

	v := Verifier{IssuerKey: issuerKeyFn(root, mid, leaf, other)}
	chain := []Cap{bogus, rootCap}
	if err := v.VerifyChain(leafCap, chain, 0x04, target, leaf.Public(), 1700000000); !errors.Is(err, ErrChainBroken) {
		t.Errorf("expected ErrChainBroken, got %v", err)
	}
}

func TestVerifyChainRejectsOpNotPermitted(t *testing.T) {
	root := mustSigner(t)
	holder := mustSigner(t)
	var target [32]byte
	c, _ := Issue(Issuance{Holder: holder.Public(), Target: target, Permissions: 0b0010, ExpiresAt: 2000000000}, root)
	v := Verifier{IssuerKey: issuerKeyFn(root, holder)}
	if err := v.VerifyChain(c, nil, 0b0100, target, holder.Public(), 1); !errors.Is(err, ErrOpNotPermitted) {
		t.Errorf("expected ErrOpNotPermitted, got %v", err)
	}
}

func TestVerifyChainEmptyChainRequiresRoot(t *testing.T) {
	root := mustSigner(t)
	holder := mustSigner(t)
	var target [32]byte
	c, _ := Issue(Issuance{Holder: holder.Public(), Target: target, Permissions: 0xFF, ExpiresAt: 2000000000}, root)
	v := Verifier{IssuerKey: issuerKeyFn(root, holder)}
	if err := v.VerifyChain(c, nil, 0x01, target, holder.Public(), 1); err != nil {
		t.Errorf("root cap with empty chain should verify: %v", err)
	}

	// Pretend it has a parent; empty chain should now fail.
	tampered := append([]byte(nil), c.Bytes()...)
	tampered[fieldAbsOff(tampered, capabilityViewParentOff)] = 0x99
	bad, err := Wrap(tampered)
	if err != nil {
		t.Fatal(err)
	}
	// Tampered buffer fails sig check first; this is OK — both are
	// "reject". We're just confirming we never silently accept.
	if err := v.VerifyChain(bad, nil, 0x01, target, holder.Public(), 1); err == nil {
		t.Errorf("VerifyChain accepted tampered cap with empty chain")
	}
}

// ----------------------------------------------------------------------------
// Revocation.
// ----------------------------------------------------------------------------

func TestRevokeAndVerify(t *testing.T) {
	signer := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000}, signer)
	r, err := Revoke(c, 1234567890, signer)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if r.CapID != c.ID() {
		t.Errorf("Revocation.CapID mismatch")
	}
	if r.RevokedAt != 1234567890 {
		t.Errorf("Revocation.RevokedAt = %d", r.RevokedAt)
	}
	if err := VerifyRevocation(r, signer.PublicKey()); err != nil {
		t.Errorf("VerifyRevocation: %v", err)
	}
}

func TestRevokeRequiresIssuerKey(t *testing.T) {
	signer := mustSigner(t)
	imposter := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000}, signer)
	if _, err := Revoke(c, 1, imposter); !errors.Is(err, ErrChainBroken) {
		t.Errorf("expected ErrChainBroken from non-issuer revoke, got %v", err)
	}
}

func TestVerifyRevocationRejectsTampered(t *testing.T) {
	signer := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000}, signer)
	r, _ := Revoke(c, 100, signer)
	r.RevokedAt = 200 // tamper
	if err := VerifyRevocation(r, signer.PublicKey()); err == nil {
		t.Errorf("tampered revocation accepted")
	}
}

// ----------------------------------------------------------------------------
// Caveat encoding round-trip for each kind.
// ----------------------------------------------------------------------------

func TestCaveatEncodingAllKinds(t *testing.T) {
	signer := mustSigner(t)
	var chainID, assetID, audience, nonce [32]byte
	for i := range chainID {
		chainID[i] = byte(i)
		assetID[i] = byte(0xA0 + i&0x0F)
		audience[i] = byte(0xC0 + i&0x0F)
		nonce[i] = byte(0xE0 + i&0x0F)
	}
	cases := []Caveat{
		{Kind: CaveatExpiresAt, Value: u64bytes(2000000000)},
		{Kind: CaveatMaxAmount, Value: u64bytes(42)},
		{Kind: CaveatDestChain, Value: chainID[:]},
		{Kind: CaveatRateLimit, Value: u32pair(120, 30)},
		{Kind: CaveatIPCIDR, Value: []byte("192.168.0.0/16")},
		{Kind: CaveatAssetID, Value: assetID[:]},
		{Kind: CaveatOpAllow, Value: u64bytes(0xF0F0F0F0)},
		{Kind: CaveatMaxDepth, Value: []byte{0x05}},
		{Kind: CaveatAudience, Value: audience[:]},
		{Kind: CaveatNonceHash, Value: nonce[:]},
	}

	c, err := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000, Caveats: cases}, signer)
	if err != nil {
		t.Fatal(err)
	}
	if c.NumCaveats() != len(cases) {
		t.Fatalf("NumCaveats = %d, want %d", c.NumCaveats(), len(cases))
	}
	for i, want := range cases {
		got := c.CaveatAt(i)
		if got.Kind != want.Kind {
			t.Errorf("[%d] Kind = %d, want %d", i, got.Kind, want.Kind)
		}
		if !bytes.Equal(got.Value, want.Value) {
			t.Errorf("[%d] Value = %x, want %x", i, got.Value, want.Value)
		}
	}
}

// ----------------------------------------------------------------------------
// v1.1 wire shape: SigSize is 3408, footer carries algorithm tag.
// ----------------------------------------------------------------------------

// TestSigSize_V1_1 freezes the wire constant: the v1.1 footer is 3408
// bytes wide (sized for FIPS 204 ML-DSA-65 + 99-byte headroom, rounded
// to 16-byte alignment). If this constant ever changes, the wire bumps
// to v1.2 and every encoder/decoder consumer must update in lockstep.
func TestSigSize_V1_1(t *testing.T) {
	if SigSize != 3408 {
		t.Fatalf("SigSize = %d, want 3408 (v1.1 wire format)", SigSize)
	}
	if AlgTagOffset != SigSize-1 {
		t.Fatalf("AlgTagOffset = %d, want SigSize-1 = %d", AlgTagOffset, SigSize-1)
	}
	if capabilityViewSize != 3572 {
		t.Fatalf("capabilityViewSize = %d, want 3572 (164 + 3408)", capabilityViewSize)
	}
	if revocationViewSize != 3448 {
		t.Fatalf("revocationViewSize = %d, want 3448 (40 + 3408)", revocationViewSize)
	}
}

// TestEd25519Signer_WritesAlgTag asserts the Ed25519 stub writes the
// SchemeEd25519 (0x02) tag at AlgTagOffset of its produced signature.
// This is the wire contract the verifier dispatches on.
func TestEd25519Signer_WritesAlgTag(t *testing.T) {
	signer := mustSigner(t)
	sig, err := signer.Sign([]byte("test payload"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if sig[AlgTagOffset] != byte(SchemeEd25519) {
		t.Errorf("sig[%d] = %#x, want SchemeEd25519 = %#x",
			AlgTagOffset, sig[AlgTagOffset], byte(SchemeEd25519))
	}
}

// TestIssueRoundTrip_AlgTagPersisted asserts that a minted cap's Sig
// field on the wire carries the algorithm tag byte the Signer set. This
// closes the loop: signer writes the tag, builder embeds it, view reads
// it back unchanged.
func TestIssueRoundTrip_AlgTagPersisted(t *testing.T) {
	signer := mustSigner(t)
	c, err := Issue(Issuance{
		Kind:        uint32(KindIAMSession),
		Permissions: 0xFF,
		ExpiresAt:   2000000000,
	}, signer)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	sig := c.Signature()
	if sig[AlgTagOffset] != byte(SchemeEd25519) {
		t.Errorf("on-wire sig[%d] = %#x, want SchemeEd25519 = %#x",
			AlgTagOffset, sig[AlgTagOffset], byte(SchemeEd25519))
	}
}

// ----------------------------------------------------------------------------
// Framing checks.
// ----------------------------------------------------------------------------

func TestWrapRejectsShortBuffer(t *testing.T) {
	if _, err := Wrap(make([]byte, 10)); !errors.Is(err, ErrTooShort) {
		t.Errorf("expected ErrTooShort, got %v", err)
	}
}

func TestWrapRejectsBadMagic(t *testing.T) {
	// Buffer big enough to clear the framing length checks but with the
	// magic field unset — should fail on the magic check.
	b := make([]byte, 512)
	if _, err := Wrap(b); !errors.Is(err, ErrBadMagic) {
		t.Errorf("expected ErrBadMagic, got %v", err)
	}
}

func TestWrapRejectsMismatchedCaveatLen(t *testing.T) {
	signer := mustSigner(t)
	c, _ := Issue(Issuance{Permissions: 1, ExpiresAt: 2000000000, Caveats: []Caveat{{Kind: CaveatMaxAmount, Value: u64bytes(1)}}}, signer)
	// Truncate one byte off the end — the ZAP size header now claims
	// more bytes than are present, so Parse must reject.
	truncated := c.Bytes()[:len(c.Bytes())-1]
	if _, err := Wrap(truncated); err == nil {
		t.Errorf("expected error from truncated buffer, got nil")
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func u64bytes(v uint64) []byte {
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		out[i] = byte(v >> (8 * i))
	}
	return out
}

func u32pair(a, b uint32) []byte {
	out := make([]byte, 8)
	for i := 0; i < 4; i++ {
		out[i] = byte(a >> (8 * i))
		out[4+i] = byte(b >> (8 * i))
	}
	return out
}

// fieldAbsOff returns the absolute byte offset of a capability field
// inside the ZAP buffer raw. fieldOff is the offset within the root
// object (one of the capabilityView*Off constants). Used by tests that
// tamper with the wire bytes to verify signature rejection.
func fieldAbsOff(raw []byte, fieldOff int) int {
	rootOff := int(raw[8]) | int(raw[9])<<8 | int(raw[10])<<16 | int(raw[11])<<24
	return rootOff + fieldOff
}
