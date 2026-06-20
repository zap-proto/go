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
//     primitive. A nil func means ed25519-only: a SchemeEd25519 tag uses
//     the built-in bootstrap verifier and every other tag (including
//     SchemeReserved / unknown) is refused fail-closed. Consumers that
//     want ML-DSA-65 / hybrid / secp256k1 wire a hook; returning
//     ErrUnhandledScheme from it for SchemeEd25519 falls back to the
//     bootstrap path, so callers need not special-case the bootstrap.
type Verifier struct {
	IsRevoked    func(capID [32]byte) bool
	IssuerKey    func(issuerHash [32]byte) ([]byte, error)
	SchemeVerify func(scheme Scheme, pub []byte, payload []byte, sig [SigSize]byte) error
}

// verifySig is the verifier-side dispatcher. It reads the algorithm tag
// at sig[AlgTagOffset] and routes to the right primitive, FAIL-CLOSED per
// SPEC §2.3 step 3c: a verifier MUST refuse any cap whose Sig algorithm
// tag it does not implement, and MUST refuse SchemeReserved (0x00) — an
// unset/zero-filled tag is never a valid scheme.
//
// Dispatch order:
//
//  1. SchemeReserved (0x00) and any tag outside the known set are
//     rejected immediately with ErrUnhandledScheme. No fallback. This is
//     the fail-closed gate.
//  2. If a SchemeVerify hook is wired, it gets first refusal on a known
//     tag; returning anything other than ErrUnhandledScheme is final
//     (this is how consumers plug ML-DSA-65 / hybrid / secp256k1).
//  3. SchemeEd25519 (0x02) falls back to the built-in bootstrap verifier
//     when the hook is absent or declines it (ed25519 is
//     mandatory-to-implement). Other known-but-unhooked schemes return
//     ErrUnhandledScheme — the verifier does not silently downgrade.
//
// The package keeps the dispatch private so the cap.Verifier surface
// stays small: callers see a single signature path; the scheme-specific
// primitive is just a callback they wire when they need PQ.
func (v Verifier) verifySig(pub []byte, payload []byte, sig [SigSize]byte) error {
	scheme := Scheme(sig[AlgTagOffset])
	if !scheme.known() {
		// Fail-closed: unknown or reserved (0x00) tag. SPEC §2.3 step 3c.
		return ErrUnhandledScheme
	}
	if v.SchemeVerify != nil {
		if err := v.SchemeVerify(scheme, pub, payload, sig); err != ErrUnhandledScheme {
			return err
		}
	}
	// Bootstrap path: ed25519 is mandatory-to-implement, so a known
	// SchemeEd25519 tag verifies here even without a hook. Any other
	// known scheme the hook declined is unhandled — never downgraded.
	if scheme == SchemeEd25519 {
		return verifyEd25519(pub, payload, sig)
	}
	return ErrUnhandledScheme
}

// Verify validates a single cap independent of chain context. Checks:
//
//   - signature is valid for the cap's Issuer (signed payload =
//     CanonicalBytes per SPEC §3: Capability[0..164) || canonical(Caveats))
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
	if err := v.verifySig(pub, c.CanonicalBytes(), c.Signature()); err != nil {
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
		// Delegation gate (SPEC §2.3 step 3d): the parent must have
		// actually authorized issuing children off it. A cap may mint a
		// child only if the parent carries PermAttenuate OR the parent is
		// itself a CapKindDelegate cap. Without this, any holder of any
		// cap could mint child caps the issuer never authorized.
		if link.Permissions()&PermAttenuate == 0 && CapKind(link.Kind()) != KindDelegate {
			return ErrNotDelegable
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
