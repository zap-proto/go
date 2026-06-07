// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
)

// Signer abstracts the issuer's signing key. Production uses ML-DSA-65
// from luxfi/crypto/pq/mldsa, returning a 3309-byte signature truncated
// to the 96-byte cap footer hash (or the constant raised — see SigSize
// in cap.go). This package keeps the dependency out and only requires
// the interface; tests plug an ed25519 implementation below.
type Signer interface {
	// Sign returns a fixed-size signature over the supplied payload.
	// The signature must verify under the Public() key on the verifier
	// side. Implementations are expected to be deterministic per call
	// for replay-debugging but must not leak the secret key.
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
// signature is 64 bytes; this stub pads it to SigSize (96) with zero
// bytes so the on-the-wire footer width is constant. The matching
// verifier (VerifyEd25519) strips the padding back off.
//
// This is intended for tests. Production should plug an ML-DSA-65 Signer.
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

// Sign produces a 64-byte ed25519 signature padded to 96 bytes.
func (s *Ed25519Signer) Sign(payload []byte) ([SigSize]byte, error) {
	var out [SigSize]byte
	sig := ed25519.Sign(s.priv, payload)
	if len(sig) != ed25519.SignatureSize {
		return out, errors.New("cap: ed25519 sign produced wrong size")
	}
	copy(out[:ed25519.SignatureSize], sig)
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
func verifyEd25519(pub []byte, payload []byte, sig [SigSize]byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("cap: ed25519 pubkey wrong size")
	}
	// Strip the 32-byte zero pad we appended in Sign.
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig[:ed25519.SignatureSize]) {
		return ErrSigMismatch
	}
	return nil
}
