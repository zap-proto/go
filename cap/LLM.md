# zap-proto/go/cap

## What this is

The Go runtime for ZAP capabilities. A `Cap` is a signed, attenuable
token of authority over a `Target`, granted to a `Holder`, by an
`Issuer`. Caps form chains via the `Parent` field; `VerifyChain` walks
back to a root checking signature, expiry, revocation, target invariance,
and monotonically-widening permissions.

Import path: `github.com/zap-proto/go/cap`. Package name: `cap`.

## Wire shape

Fixed-offset prefix (168 bytes) + length-prefixed caveat block + 96-byte
trailing signature. Field offsets are baked in so `Cap` accessors are
O(1) and zero-allocation. `Cap.Bytes()` returns the raw buffer with no
copy. `Cap.SignedBytes()` is the slice the signature covers
(everything but the trailing footer). Schema lives at
`github.com/zap-proto/zap-spec/capabilities.zap`.

## API

- `Wrap(b []byte) (Cap, error)` — parse a buffer (no allocation).
- `Issue(in Issuance, signer Signer) (Cap, error)` — mint a root cap.
- `Attenuate(parent, holder, perms, caveats, expiresAt, signer)` —
  derive a child. Permissions intersect with parent's, expiry can only
  shrink, signer must equal `parent.Holder()`.
- `Verifier{IsRevoked, IssuerKey}.Verify(c, now)` — single-cap checks.
- `Verifier.VerifyChain(leaf, chain, op, target, holder, now)` —
  full-chain validation including op-against-mask.
- `Revoke(c, now, signer)` / `VerifyRevocation(r, pub)` — revocation.

## Crypto

Hash: SHA-256 (BLAKE3 in v1.1 — see `Hash32` swap-point comment).
Signature: pluggable `Signer` interface. Production plugs ML-DSA-65 from
`luxfi/crypto/pq/mldsa`; tests use `Ed25519Signer` (64-byte sig padded
to the 96-byte footer width).

## Layout

```
cap.go     Cap zero-copy view + accessors + ID + Hash32
issue.go   Issuance, Issue, Attenuate, buildCapBytes
verify.go  Verifier, Verify, VerifyChain
revoke.go  Revocation, Revoke, VerifyRevocation
signer.go  Signer interface + Ed25519Signer test stub
cap_test.go  Round-trip, attenuation, chain walk, revocation, caveat kinds
```

## Test

```
go test ./cap/
```

All tests must pass clean.
