# zap-proto/go

Canonical Go runtime for the ZAP wire format. See
[github.com/zap-proto/spec](https://github.com/zap-proto/spec) for the
format specification.

## Promise pipelining

ZAP RPC pipelines on the call envelope's `Target` field (the ONE canonical
model, shared byte-for-byte with the `@zap-proto/zap` TypeScript runtime and the
`zap-proto` Python runtime). A
call carries a caller-assigned `PromiseID`; a dependent call sets `Target` to
a prior call's `PromiseID`, and the server substitutes that prior call's
resolved result for the dependent's payload before dispatching it — so the
dependent ships without waiting for the first answer to round-trip back.

```go
sess := rpc.NewSession()
p := sess.Next()
srv := rpc.NewPipeliner(DispatchEcho(handler)) // wraps a generated Dispatch<Iface>

// A: authenticate. B: a call pipelined on A's answer (Target = A's PromiseID).
aResp, _ := srv.Handle(rpc.BuildRequest(sess.Origin(p, AuthOrdinal, cap, nil)))
q := sess.Next()
bResp, _ := srv.Handle(rpc.BuildRequest(sess.Pipeline(q, p, GetOrdinal, cap, nil)))
```

`rpc.Pipeliner` resolves a `Target`, queues a dependent whose target has not
resolved yet until it does, and refuses (`StatusBadRequest`) one whose target
answered non-OK or was `Finish`ed. Generated clients expose this as `M(...)`
(originating) and `MOn(on rpc.Promise)` (dependent). A non-pipelined call uses
`Target = NoTarget`, so a `Pipeliner` and a plain dispatcher are wire-compatible.

The Rust stack (`zap-rpc`) implements the richer capnp `PromisedAnswer`
(transform-path) model — a superset that interoperates at the envelope level
for non-pipelined calls. See `LLM.md` for the full model and boundary.

## Capabilities

Package `cap` is the canonical ZAP capability runtime — signed, attenuable
tokens of authority. A `Cap` grants a `Holder` a `Permissions` bitmask over a
`Target`, issued by an `Issuer`; caps form a chain via `Parent`, and
`VerifyChain` walks back to a root checking each signature, expiry, revocation,
target invariance, and monotonically-narrowing permissions.

```go
import "github.com/zap-proto/go/cap"

signer, _ := cap.NewEd25519Signer()
root, _ := cap.Issue(cap.Issuance{
	Kind:        uint32(cap.KindIAMSession),
	Permissions: cap.PermAttenuate, // may exercise *and* delegate
	ExpiresAt:   2_000_000_000,
}, signer)

// Derive a narrower child (permissions intersect, expiry only shrinks, parent
// must carry PermAttenuate or be a KindDelegate cap).
child, _ := cap.Attenuate(root, childHolder, cap.PermAudit, nil, 0, signer)
```

`Issue` / `Attenuate` enforce the SPEC §2.3 delegation gate at mint time;
`Verifier.Verify` / `VerifyChain` enforce the full invariants with **fail-closed**
scheme dispatch (reserved tag `0x00` and any unimplemented tag are refused, never
downgraded). The signed scope is `Capability[0..164) || canonical(Caveats)` and
the CapID is `SHA-256(canonicalBytes || Sig)` — byte-identical to the Python,
Rust, and TypeScript runtimes (pinned by a shared known-answer test). Ed25519 is
the built-in bootstrap scheme; ML-DSA-65 / hybrid / secp256k1 are wired via a
`SchemeVerify` hook + matching `Signer`. The capability layer ships in all four
reference runtimes (Go, Python, Rust, TypeScript). Spec:
[`zap-proto/zap-spec`](https://github.com/zap-proto/zap-spec) `SPEC.md`
§2.3 / §3 / §4; full model in `cap/LLM.md`.
