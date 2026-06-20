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
(3572 bytes at v1.1: Kind…Sig at the offsets declared in
`capabilities.zap`) followed by the Caveats list elements (each a full
ZAP sub-message length-prefixed by `AddObjectBytes`). No "ZCAP" magic;
no hand-rolled length prefixes. The wire bytes are produced by the
generated `NewCapabilityView` builder and read by the generated
`CapabilityView` zero-copy view in `capabilities_zap.go` — `cap.go`
is a thin idiomatic wrapper exposing the public `Cap` type.

Signature scope: SPEC §3 canonical bytes — `Capability[0..164)`
(Kind through the Caveats list pointer) concatenated with each Caveat
encoded `Kind:u32-LE || len(Value):u32-LE || Value` in list order.
`Cap.CanonicalBytes()` produces them; the signer and verifier share that
one definition (no "build relaxed, verify strict" asymmetry), and the
encoding excludes `Sig` and the ZAP heap-indirection bytes so it is
identical across language runtimes. `CapID = Hash32(CanonicalBytes ||
Sig)`. Schema lives at
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
  shrink, signer must equal `parent.Holder()`, and the parent must carry
  `PermAttenuate` (or be `KindDelegate`) — else `ErrNotDelegable`.
- `Cap.CanonicalBytes()` — the SPEC §3 signing scope (see Wire shape).
- `Verifier{IsRevoked, IssuerKey, SchemeVerify}.Verify(c, now)` —
  single-cap checks (signature fail-closed on unknown scheme).
- `Verifier.VerifyChain(leaf, chain, op, target, holder, now)` —
  full-chain validation including op-against-mask and the delegation gate.
- `Revoke(c, now, signer)` / `VerifyRevocation(r, pub)` /
  `Verifier.VerifyRevocation(r, pub)` — revocation (scheme-aware).

## Crypto

Hash: SHA-256 (BLAKE3 swap-point planned — see `Hash32` comment).
Signature: pluggable `Signer` interface (sign side) + `Verifier.SchemeVerify`
hook (verify side). The wire scheme is selected by the algorithm tag at
`Sig[AlgTagOffset]`; dispatch is fail-closed (SPEC §2.3 step 3c) — a tag
the verifier does not implement, or `SchemeReserved` (0x00), is refused.
Ed25519 is the built-in bootstrap (mandatory-to-implement); a consumer
wires ML-DSA-65 / hybrid / secp256k1 by supplying a `SchemeVerify` hook
(and the matching `Signer`). Tests use the in-package `Ed25519Signer`
(64-byte sig padded to the SigSize-byte footer width, scheme tag
`SchemeEd25519` at `Sig[AlgTagOffset]`).

Permission bits: the cross-cutting top-32 bits are defined in `perms.go`
(`PermAttenuate` 1<<32, `PermAudit` 1<<33, `PermRoot` 1<<63). Attenuation
is gated on `PermAttenuate` (or `KindDelegate`) at BOTH mint time
(`Attenuate`) and verify time (`VerifyChain`, SPEC §2.3 step 3d).

## Layout

```
capabilities_zap.go  Generated zero-copy views + builders (DO NOT EDIT)
cap.go               Public Cap wrapper + accessors + Scheme + ID + Hash32
canonical.go         CanonicalBytes — the SPEC §3 signing scope
perms.go             Cross-cutting Permission bits (PermAttenuate/Audit/Root)
issue.go             Issuance, Issue, Attenuate, buildCapBytes
verify.go            Verifier, Verify, VerifyChain (delegation gate)
revoke.go            Revocation, Revoke, VerifyRevocation,
                       EncodeRevocation, DecodeRevocation
signer.go            Signer interface + Ed25519Signer test stub
cap_test.go          Round-trip, attenuation, chain walk, revocation, caveats
spec_test.go         SPEC conformance: delegation gate, canonical bytes,
                       scheme-aware + fail-closed signature dispatch
```

## Test

```
go test ./cap/
```

All tests must pass clean.
