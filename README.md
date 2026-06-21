# zap-proto/go

Canonical Go runtime for the ZAP wire format. See
[github.com/zap-proto/spec](https://github.com/zap-proto/spec) for the
format specification.

## Promise pipelining

ZAP RPC pipelines on the call envelope's `Target` field (the ONE canonical
model, shared with the `@zap-proto/zap` TypeScript runtime byte-for-byte). A
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
