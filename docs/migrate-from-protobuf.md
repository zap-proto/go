# Migrating from Protocol Buffers + gRPC to ZAP

This is the guide we wrote while moving a 450k-line distributed storage system
(Hanzo S3, a SeaweedFS fork) off protobuf and gRPC. It is opinionated and
battle-tested.

## Why

- **No encode step.** A protobuf message is a Go struct you `Marshal` to bytes
  and `Unmarshal` back. A ZAP message *is* the bytes — `New<Msg>(...)` builds
  the wire buffer directly; `Wrap<Msg>(buf)` reads fields in place. Build once,
  read many. Zero copy.
- **No gRPC.** RPC rides [`transport`](../transport) — TCP, Unix, or QUIC, all
  PQ-secured (X25519MLKEM768). One `Conn`/`Dispatch` contract; the generated
  client takes the `*Conn` directly.
- **Post-quantum by default**, pure Go, no liboqs, no CGO in the core.

## The model — three layers, decomplected

```
schema   (.zap)        ──zapgen──▶   <msg>_zap.go      // build/read, zero-copy
rpc      (envelopes)                 rpc.Call/Response  // promise pipelining
transport(connections)               transport.Conn     // tcp/unix/quic + PQ
```

Each layer is one concern. A message knows nothing about transport; the
transport carries opaque envelopes; the rpc layer correlates them.

## The toolchain

```
.proto ──[pb2zap -emit zap]──▶ .zap ──[zapgen]──▶ _zap.go
            (one-time bootstrap)    (canonical source)   (generated)
```

1. **[pb2zap](https://github.com/zap-proto/pb2zap)** bootstraps native `.zap`
   schemas from your existing `.proto` (then delete the `.proto` — `.zap` is the
   source of truth), and rewrites Go call sites off the `*_pb` packages.
2. **zapgen** generates the zero-copy `_zap.go` from `.zap`.

## The cut pattern (per service)

The mechanical shape — copy it everywhere:

```go
// client:  pb.NewFooClient(grpcConn)            -> transport.Dial + foowire client
conn, _ := transport.Dial("unix", sock)          // or DialTLS(addr, PQTLSConfig(cfg))
client := foowire.NewFooClient(conn, nil)
_, body, _ := client.Bar(foowire.NewBarRequest(foowire.BarRequestInput{X: v}))
resp, _ := foowire.WrapBarResponse(body)
use(resp.Y())                                     // accessor, not resp.Y

// server:  pb.RegisterFooServer(grpcS, impl)    -> transport.Serve + DispatchFoo
transport.Serve(listener, func(env []byte) ([]byte, error) {
    return foowire.DispatchFoo(handler, env)
})
// handler:  (ctx, *pb.BarRequest) -> (*pb.BarResponse, error)
//   becomes (req []byte) -> ([]byte, error)   via Wrap/New
```

Streaming: `client.OpenStream(ordinal, init)` + `stream.Send`/`Recv` (io.EOF on
half-close); server `transport.ListenStream(...)`. Chained reads (lookup →
list → read) use **promise pipelining** (`MethodOn(promise)`) — the server
resolves the chain without round-trips, something request/response protobuf
could not express.

## Strategy — strangler, never big-bang

Migrate **service by service**, keeping the tree green at every commit:

1. Generate the wire package for the service (`.proto → .zap → _zap.go`).
2. Rewrite its call sites to wire + transport — `pb2zap` for construction,
   then by hand for the field reads (immutable ZAP has no field *write*; reads
   become accessors). Leave the `*_pb` package in place.
3. Once nothing imports `<svc>_pb`, delete the package and its `.proto`.
4. When the last service is cut, drop `grpc` + `protobuf` from `go.mod`.

The honest part: construction is mechanical, but **field-access conversion is a
data-flow redesign** (`*pb.Msg` is one type for build+read; ZAP splits it into
Input → bytes → View). Budget for it. The deeply-woven services (a metadata
filer, a volume server) are real refactoring, not a codemod — `pb2zap` does the
safe part and reports the rest.

## Checklist

- [ ] `.zap` schemas generated, `.proto` deleted, schemas committed as source
- [ ] each service: client dials `transport`, server serves `Dispatch<Svc>`
- [ ] streaming methods on `OpenStream`/`ListenStream`
- [ ] `grep -r '_pb"'` empty (outside generated), `grpc`/`protobuf` out of go.mod
- [ ] PQ on the wire: `DialTLS(..., transport.PQTLSConfig(...))` or QUIC
