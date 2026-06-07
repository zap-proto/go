// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
)

// Signer abstracts the issuer's signing key. The v1.1 wire format's
// signature footer (SigSize bytes, see cap.go) is wide enough for any
// supported primitive: secp256k1 ECDSA (65 B), Ed25519 (64 B), or
// ML-DSA-65 (3309 B per FIPS 204 §5.2). Implementations write their
// scheme tag at sig[AlgTagOffset] before signing so verifiers can
// dispatch on it.
//
// This package only requires the interface; concrete signers live in
// the runtime's consumers (e.g. github.com/hanzoai/iam/capauth) where
// the appropriate crypto dependency is wired in. The Ed25519Signer
// stub below is provided for tests and bootstrap.
type Signer interface {
	// Sign returns a fixed-size signature over the supplied payload.
	// The signature MUST verify under the Public() key on the verifier
	// side. Implementations SHOULD be deterministic per call for
	// replay-debugging and MUST NOT leak the secret key. The final
	// byte (sig[AlgTagOffset]) MUST carry the algorithm tag.
	Sign(payload []byte) ([SigSize]byte, error)

	// Public returns the canonical 32-byte hash of the signer's public
	// key. This must match the cap's Issuer field for Verify to accept
	// the signature.
	Public() [32]byte
}

// Verifier-side signature checker. The cap package keeps signing and
// verification symmetric: the same encoding produced by Signer.Sign must
// be accepted by VerifySig with the resolved pubkey.
type sigVerifier func(pub []byte, payload []byte, sig [SigSize]byte) error

// ---- ed25519 test stub ----------------------------------------------------

// Ed25519Signer is a Signer backed by an ed25519 key. ed25519's native
// signature is 64 bytes; this stub places it at the leading bytes of
// the SigSize footer with the remaining bytes zero-padded and the
// algorithm tag (SchemeEd25519 = 0x02) at sig[AlgTagOffset]. The
// matching verifier (verifyEd25519) reads the leading 64 bytes back
// out and ignores the pad and tag byte.
//
// This is intended for tests and bootstrap. Production PQ deployments
// plug an ML-DSA-65 Signer via the runtime's consumer (e.g.
// github.com/hanzoai/iam/capauth.MLDSA65Signer).
type Ed25519Signer struct {
	priv ed25519.PrivateKey
	pub  [32]byte
}

// NewEd25519Signer generates a fresh keypair.
func NewEd25519Signer() (*Ed25519Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	s := &Ed25519Signer{priv: priv}
	s.pub = Hash32(pub)
	return s, nil
}

// Sign produces a 64-byte ed25519 signature placed at the leading bytes
// of the SigSize footer and tagged with SchemeEd25519 at AlgTagOffset.
func (s *Ed25519Signer) Sign(payload []byte) ([SigSize]byte, error) {
	var out [SigSize]byte
	sig := ed25519.Sign(s.priv, payload)
	if len(sig) != ed25519.SignatureSize {
		return out, errors.New("cap: ed25519 sign produced wrong size")
	}
	copy(out[:ed25519.SignatureSize], sig)
	out[AlgTagOffset] = byte(SchemeEd25519)
	return out, nil
}

// Public returns the 32-byte hash of the ed25519 public key.
func (s *Ed25519Signer) Public() [32]byte { return s.pub }

// PublicKey returns the raw 32-byte ed25519 public key. Used in tests to
// register the signer with a Verifier's IssuerKey lookup.
func (s *Ed25519Signer) PublicKey() ed25519.PublicKey {
	return s.priv.Public().(ed25519.PublicKey)
}

// verifyEd25519 checks a padded ed25519 signature against a raw pubkey.
// The signature occupies sig[:ed25519.SignatureSize]; the bytes between
// it and sig[AlgTagOffset] are zero pad ignored by this verifier.
func verifyEd25519(pub []byte, payload []byte, sig [SigSize]byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("cap: ed25519 pubkey wrong size")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig[:ed25519.SignatureSize]) {
		return ErrSigMismatch
	}
	return nil
}
