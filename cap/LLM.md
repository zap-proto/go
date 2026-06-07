# zap-proto/go/cap

## What this is

The Go runtime for ZAP capabilities. A `Cap` is a signed, attenuable
token of authority over a `Target`, granted to a `Holder`, by an
`Issuer`. Caps form chains via the `Parent` field; `VerifyChain` walks
back to a root checking signature, expiry, revocation, target invariance,
and monotonically-widening permissions.

Import path: `github.com/zap-proto/go/cap`. Package name: `cap`.

## Wire shape

Canonical ZAP framing — 16-byte ZAP header (magic + version + root
offset + size) followed by the Capability object's fixed section
(260 bytes: Kind…Sig at the offsets declared in `capabilities.zap`)
followed by the Caveats list elements (each a full ZAP sub-message
length-prefixed by `AddObjectBytes`). No "ZCAP" magic; no hand-rolled
length prefixes. The wire bytes are produced by the generated
`NewCapabilityView` builder and read by the generated `CapabilityView`
zero-copy view in `capabilities_zap.go` — `cap.go` is a thin idiomatic
wrapper exposing the public `Cap` type.

Signature scope: the full ZAP buffer with the 96-byte Sig field zeroed.
`Cap.SignedBytes()` allocates a copy with the sig field zeroed and
returns that. Schema lives at
`github.com/zap-proto/zap-spec/capabilities.zap`.

`capabilities_zap.go` is `go generate`-regenerable:

```
zapgen -single -type-suffix=View -out . path/to/capabilities.zap
```

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
capabilities_zap.go  Generated zero-copy views + builders (DO NOT EDIT)
cap.go               Public Cap wrapper + accessors + ID + Hash32
issue.go             Issuance, Issue, Attenuate, buildCapBytes
verify.go            Verifier, Verify, VerifyChain
revoke.go            Revocation, Revoke, VerifyRevocation,
                       EncodeRevocation, DecodeRevocation
signer.go            Signer interface + Ed25519Signer test stub
cap_test.go          Round-trip, attenuation, chain walk, revocation, caveats
```

## Test

```
go test ./cap/
```

All tests must pass clean.
