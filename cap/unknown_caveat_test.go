package cap

import (
	"errors"
	"testing"
)

// TestUnknownCaveatKindFailsClosed pins SPEC §2.3 line 106: a verifier MUST
// refuse a cap carrying an unknown CaveatKind (fail-closed). A caveat is a
// restriction; an unrecognized one cannot be evaluated, so accepting the cap
// would silently ignore a constraint the issuer intended.
//
// This was a real cross-language divergence found by red-team review: Go's
// Verify formerly ACCEPTED an unknown caveat kind while the Rust verifier
// REJECTED the identical bytes. Go now matches Rust + the spec.
func TestUnknownCaveatKindFailsClosed(t *testing.T) {
	signer := mustSigner(t)
	const unknownKind = 0x42424242 // NOT in {0x00..0x09}
	c, err := Issue(Issuance{
		Kind:        uint32(KindKMSAccess),
		Permissions: 0xFF,
		IssuedAt:    1700000000,
		ExpiresAt:   2000000000,
		Caveats: []Caveat{
			{Kind: CaveatKind(unknownKind), Value: []byte("must-not-be-ignored")},
		},
	}, signer)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	v := Verifier{IssuerKey: issuerKeyFn(signer)}

	if verr := v.Verify(c, 1700000001); !errors.Is(verr, ErrUnknownCaveat) {
		t.Fatalf("Verify: want ErrUnknownCaveat (fail-closed), got %v", verr)
	}

	// VerifyChain must reject it too (it calls Verify on the leaf + each link).
	if cerr := v.VerifyChain(c, nil, 0x01, c.Target(), c.Holder(), 1700000001); !errors.Is(cerr, ErrUnknownCaveat) {
		t.Fatalf("VerifyChain: want ErrUnknownCaveat, got %v", cerr)
	}
}

// TestKnownCaveatKindsAccepted is the positive control: every defined kind
// (0x00..0x09) must still pass the kind gate.
func TestKnownCaveatKindsAccepted(t *testing.T) {
	signer := mustSigner(t)
	for kind := CaveatExpiresAt; kind <= CaveatNonceHash; kind++ {
		c, err := Issue(Issuance{
			Kind:        uint32(KindKMSAccess),
			Permissions: 0xFF,
			IssuedAt:    1700000000,
			ExpiresAt:   2000000000,
			Caveats:     []Caveat{{Kind: kind, Value: []byte("ok")}},
		}, signer)
		if err != nil {
			t.Fatalf("Issue kind %#x: %v", kind, err)
		}
		v := Verifier{IssuerKey: issuerKeyFn(signer)}
		if err := v.Verify(c, 1700000001); errors.Is(err, ErrUnknownCaveat) {
			t.Fatalf("Verify rejected KNOWN caveat kind %#x as unknown", kind)
		}
	}
}
