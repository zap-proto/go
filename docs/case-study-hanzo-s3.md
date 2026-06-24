# Case study: Hanzo S3 — two Raft impls + protobuf/gRPC → Lux consensus + ZAP

Hanzo S3 is a 450k-line distributed object store (a SeaweedFS fork). It carried
the full legacy coordination + RPC stack. This is what came out, what went in,
and — honestly — what's still in flight.

## Before

| Layer | What it was |
|---|---|
| Coordination | **two** Raft implementations — `seaweedfs/raft` *and* `hashicorp/raft` — for master HA: leader election, snapshots, a gRPC raft transport |
| RPC | gRPC across **14 services**, **166 RPCs** (34 streaming) |
| Messages | protobuf — **534 message types**, marshal/unmarshal on every call |
| Extras | Apache **thrift** (dragged in by an Iceberg backend), `go.mod` carrying a local `replace` to a private path |

Two leader-election FSMs to reason about. A marshal step on every read. gRPC's
request/response forcing round-trips on naturally-chained metadata ops. A
post-quantum story: none.

## After

| Layer | What it is |
|---|---|
| Coordination | **one** `replog` over Lux consensus — leaderless, permissionless, PQ-final (Quasar BLS + ML-DSA). The master's entire FSM is `apply(MaxVolumeIdCommand)`. |
| Writer | non-commutative ops (volume-id allocation, GC) gated on a **deterministic pinned writer** — no election, no forward-to-leader |
| RPC | `zap-proto/go/transport` — TCP/Unix/QUIC, **PQ X-Wing (X25519MLKEM768)**, one `Conn`/`Dispatch` contract |
| Messages | **565 native ZAP schemas**, zero-copy: `New<Msg>` builds the wire buffer, `Wrap<Msg>` reads in place. No marshal. |
| Extras | thrift + the Iceberg backend **deleted**; `go.mod` on a **public** pinned version, zero local replaces |

## The wins, concretely

- **2 Raft impls → 0.** The master coordinates on `replog.Commit`; ~600 lines of
  election/snapshot/bootstrap logic become an `apply` func + a 7-line writer.
- **No encode step.** Reads are zero-copy slices of the received buffer.
- **Leaderless** — no election stalls, no leader as a single point of failure,
  no client forward-to-leader hop.
- **Post-quantum by default** on every connection; pure Go, no CGO in the core,
  no liboqs.
- **Promise pipelining** — chained metadata reads (lookup → list → read) resolve
  server-side without round-trips, which protobuf request/response could not do.
- **6 `_pb` packages deleted**, thrift + a whole backend gone from the build.

## What's honest about it

The migration is **not finished**, and pretending otherwise would be the slop we
ban. The foundation, the consensus, and the tractable services (mount, mq_agent,
s3, s3_lifecycle, …) are cut and green. The deeply-woven giants — the metadata
**filer** (~5,300 message-field references), **master**, **volume_server** — are
in flight.

The lesson that earned that honesty: **pb→ZAP is a data-flow redesign, not a
codemod.** A `*pb.Entry` is one type used to both build and read; ZAP splits it
into `Input` → `[]byte` → `View`. Construction rewrites mechanically
([`pb2zap`](https://github.com/zap-proto/pb2zap) does it); the 5,000 field-access
sites are genuine refactoring — build-once / read-many. We strangler-migrate
service by service, tree green at every commit, and tooling does the safe part
while a human owns the redesign.

## The toolchain that made it tractable

```
.proto ──pb2zap──▶ .zap ──zapgen──▶ _zap.go        # schema, zero-copy
                          transport + replog        # RPC over PQ, consensus
```

See the [protobuf → ZAP](migrate-from-protobuf.md) and
[Raft → Lux](https://github.com/luxfi/consensus/blob/main/docs/raft-to-lux.md)
guides.

## Footnote: the rest of the cloud stack

The heavy lift was the storage fork. A sweep of the Hanzo cloud stack found it
already ZAP-clean: `iam` runs on `zap-proto/go`; `platform`, `kms`, `mcp`,
`extension` carry no grpc/protobuf/raft/zk at all. ZAP-native isn't an aspiration
there — it's the default.
