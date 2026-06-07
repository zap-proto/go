// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

// Verifier holds the policy dependencies cap validation needs:
//
//   - IsRevoked is a side-channel lookup against the revocation list.
//     Return true to reject the cap regardless of signature or expiry.
//     A nil func is treated as "nothing revoked".
//
//   - IssuerKey resolves an issuer's 32-byte hash back to its raw public
//     key bytes (ed25519 raw pubkey or ML-DSA-65 FIPS 204 encoding).
//     Must return ErrIssuerUnknown for unknown issuers.
//
//   - SchemeVerify dispatches on the algorithm-tag byte at
//     sig[AlgTagOffset] to validate the signature under the right
//     primitive. A nil func defaults to ed25519-only (the bootstrap
//     scheme); consumers that want ML-DSA-65 or hybrid wire a dispatch
//     table whose default falls back to the package-private ed25519
//     path so callers do not need to special-case the bootstrap.
type Verifier struct {
	IsRevoked    func(capID [32]byte) bool
	IssuerKey    func(issuerHash [32]byte) ([]byte, error)
	SchemeVerify func(scheme Scheme, pub []byte, payload []byte, sig [SigSize]byte) error
}

// verifySig is the verifier-side dispatcher. It reads the algorithm tag
// at sig[AlgTagOffset], runs the SchemeVerify hook if set, and falls
// back to ed25519 (the bootstrap scheme, mandatory-to-implement) when
// the hook is nil OR returns "scheme not handled".
//
// The package keeps the dispatch private so the cap.Verifier surface
// stays small: callers see a single signature path; the scheme-specific
// primitive is just a callback they wire when they need PQ.
func (v Verifier) verifySig(pub []byte, payload []byte, sig [SigSize]byte) error {
	scheme := Scheme(sig[AlgTagOffset])
	if v.SchemeVerify != nil {
		if err := v.SchemeVerify(scheme, pub, payload, sig); err != ErrUnhandledScheme {
			return err
		}
	}
	// Bootstrap path: ed25519. Untagged buffers (scheme==0) still hit
	// this fallback because the legacy Ed25519Signer.Sign predated the
	// tag-byte convention and consumers may still produce zero-tagged
	// caps during the v1.0→v1.1 transition. Once every consumer writes
	// the tag, callers MAY wire a SchemeVerify that refuses scheme==0.
	if scheme == SchemeEd25519 || scheme == SchemeReserved {
		return verifyEd25519(pub, payload, sig)
	}
	return ErrUnhandledScheme
}


// Verify validates a single cap independent of chain context. Checks:
//
//   - signature is valid for the cap's Issuer (signed payload =
//     the full ZAP buffer with the Sig field zeroed)
//   - not expired at the supplied now (unix seconds)
//   - not revoked
//   - caveat list parses cleanly
//
// Returns nil if the cap is acceptable. Note that Verify does NOT walk
// the parent chain — use VerifyChain for that. A cap that passes Verify
// is structurally sound but may still be useless if its parent is
// revoked or its permissions don't include the op being attempted.
func (v Verifier) Verify(c Cap, now int64) error {
	// Walk the caveat list once to catch bad framing. Each element is a
	// full ZAP sub-message; ObjectAt returns Object{} on bad bytes.
	list := c.view.Caveats()
	for i := 0; i < list.Length(); i++ {
		sub := list.ObjectAt(i)
		if sub.IsNull() {
			return ErrBadCaveats
		}
	}

	// Expiry check. 0 means "never expires".
	if exp := c.ExpiresAt(); exp != 0 && uint64(now) > exp {
		return ErrExpired
	}

	// Revocation check.
	id := c.ID()
	if v.IsRevoked != nil && v.IsRevoked(id) {
		return ErrRevoked
	}

	// Signature check.
	if v.IssuerKey == nil {
		return ErrIssuerUnknown
	}
	issuerHash := c.Issuer()
	pub, err := v.IssuerKey(issuerHash)
	if err != nil {
		return err
	}
	if len(pub) == 0 {
		return ErrIssuerUnknown
	}
	if err := v.verifySig(pub, c.SignedBytes(), c.Signature()); err != nil {
		return err
	}

	return nil
}

// VerifyChain validates a cap proof end-to-end:
//
//   - leaf has not expired, is not revoked, has a valid signature
//   - leaf grants op (bit set in Permissions)
//   - leaf grants target (matches the supplied target)
//   - leaf grants holder (matches the supplied holder)
//   - chain links each parent ID to the next cap in chain
//   - every link verifies (signature, expiry, revocation)
//   - every link's permissions are a superset of the child's
//   - the root link (chain[len-1]) has Parent == zero
//   - caveats are walked at each level
//
// Pass chain as parents nearest-to-leaf first: chain[0] is the leaf's
// parent, chain[len-1] is the root. An empty chain means leaf is a root
// (and the function only checks the leaf itself).
func (v Verifier) VerifyChain(leaf Cap, chain []Cap, op uint64, target [32]byte,
	holder [32]byte, now int64) error {
	if err := v.Verify(leaf, now); err != nil {
		return err
	}
	if leaf.Target() != target {
		return ErrTargetMismatch
	}
	if leaf.Holder() != holder {
		return ErrHolderMismatch
	}
	if leaf.Permissions()&op == 0 {
		return ErrOpNotPermitted
	}

	prev := leaf
	for i, link := range chain {
		// The current cap's Parent must equal this link's ID.
		linkID := link.ID()
		if prev.Parent() != linkID {
			return ErrChainBroken
		}
		// This link must be valid on its own merits.
		if err := v.Verify(link, now); err != nil {
			return err
		}
		// Authority must monotonically widen as we walk toward the root.
		// The child's permissions must be a subset of the parent's; the
		// child's issuer must equal the parent's holder.
		if prev.Permissions()&link.Permissions() != prev.Permissions() {
			return ErrPermsExceedPar
		}
		if prev.Issuer() != link.Holder() {
			return ErrChainBroken
		}
		// Target must remain identical as authority is attenuated.
		if link.Target() != target {
			return ErrTargetMismatch
		}
		// The last link must be a root (Parent zero).
		if i == len(chain)-1 {
			var zero [32]byte
			if link.Parent() != zero {
				return ErrChainBroken
			}
		}
		prev = link
	}
	// If chain is empty, leaf must itself be a root.
	if len(chain) == 0 {
		var zero [32]byte
		if leaf.Parent() != zero {
			return ErrChainBroken
		}
	}
	return nil
}
