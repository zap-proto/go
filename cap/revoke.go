// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import (
	"encoding/binary"
)

// Revocation is the on-the-wire record stating that a particular cap is
// no longer valid. Revocations are gossiped/published out-of-band; the
// canonical store is a hash-set keyed on CapID.
//
// The signature is over a 40-byte canonical payload:
//
//	[32]byte CapID || uint64 RevokedAt (little-endian)
//
// Padded to SigSize in the same shape as cap signatures so the verifier
// can reuse one signing primitive. The wire encoding uses ZAP framing
// via the generated RevocationView; this struct is the idiomatic Go
// container used at the boundaries.
type Revocation struct {
	CapID      [32]byte
	RevokedAt  int64
	RevokerSig [SigSize]byte
}

// revocationPayload serializes the bytes that get signed.
func revocationPayload(capID [32]byte, revokedAt int64) []byte {
	out := make([]byte, 40)
	copy(out[:32], capID[:])
	binary.LittleEndian.PutUint64(out[32:], uint64(revokedAt))
	return out
}

// Revoke produces a Revocation record signed by signer. The signer must
// be the cap's original issuer — only the issuer (or a delegated
// revocation authority, out of scope here) can revoke a cap.
func Revoke(c Cap, now int64, signer Signer) (Revocation, error) {
	if signer == nil {
		return Revocation{}, errMissingSigner
	}
	if signer.Public() != c.Issuer() {
		return Revocation{}, ErrChainBroken
	}
	id := c.ID()
	sig, err := signer.Sign(revocationPayload(id, now))
	if err != nil {
		return Revocation{}, err
	}
	return Revocation{
		CapID:      id,
		RevokedAt:  now,
		RevokerSig: sig,
	}, nil
}

// VerifyRevocation checks that r is a valid revocation under issuerPub.
// The caller is expected to have resolved the original cap's Issuer
// hash to a public key (via the same IssuerKey lookup the Verifier
// uses) and then call this with the resolved key.
func VerifyRevocation(r Revocation, issuerPub []byte) error {
	return verifyEd25519(issuerPub, revocationPayload(r.CapID, r.RevokedAt), r.RevokerSig)
}

// EncodeRevocation marshals a Revocation into canonical ZAP wire bytes
// via the generated RevocationView builder. Symmetric to DecodeRevocation.
func EncodeRevocation(r Revocation) []byte {
	return NewRevocationView(RevocationViewInput{
		CapID:      r.CapID,
		RevokedAt:  uint64(r.RevokedAt),
		RevokerSig: r.RevokerSig,
	})
}

// DecodeRevocation parses a ZAP-framed Revocation buffer back into the
// idiomatic Go struct. Returns ErrBadMagic / ErrTooShort on framing
// errors. Symmetric to EncodeRevocation.
func DecodeRevocation(b []byte) (Revocation, error) {
	v, err := WrapRevocationView(b)
	if err != nil {
		return Revocation{}, err
	}
	return Revocation{
		CapID:      v.CapID(),
		RevokedAt:  int64(v.RevokedAt()),
		RevokerSig: v.RevokerSig(),
	}, nil
}
