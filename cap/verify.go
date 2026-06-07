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
//     key bytes (ed25519 today; ML-DSA-65 in production). Must return
//     ErrIssuerUnknown for unknown issuers.
type Verifier struct {
	IsRevoked func(capID [32]byte) bool
	IssuerKey func(issuerHash [32]byte) ([]byte, error)
}

// Verify validates a single cap independent of chain context. Checks:
//
//   - signature is valid for the cap's Issuer
//   - not expired at the supplied now (unix seconds)
//   - not revoked
//   - caveat block parses cleanly
//
// Returns nil if the cap is acceptable. Note that Verify does NOT walk
// the parent chain — use VerifyChain for that. A cap that passes Verify
// is structurally sound but may still be useless if its parent is
// revoked or its permissions don't include the op being attempted.
func (v Verifier) Verify(c Cap, now int64) error {
	// Caveat block sanity: walk it once. NumCaveats and CaveatsLen are
	// authoritative; if walking blows out the bounds the cap is junk.
	n := c.NumCaveats()
	end := offCaveats + c.caveatsLen()
	p := offCaveats
	for k := 0; k < n; k++ {
		if p+8 > end {
			return ErrBadCaveats
		}
		vlen := int(c.raw[p+4]) | int(c.raw[p+5])<<8 |
			int(c.raw[p+6])<<16 | int(c.raw[p+7])<<24
		if p+8+vlen > end {
			return ErrBadCaveats
		}
		p += 8 + vlen
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
	if err := verifyEd25519(pub, c.SignedBytes(), c.Signature()); err != nil {
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
