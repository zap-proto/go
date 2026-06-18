# zap-proto/go

## What this is

The canonical Go runtime for the ZAP wire format. Pure stdlib, zero
external dependencies. Provides the read side (`Parse`, `Message`,
`Object`, `List`) and the write side (`Builder`, `ObjectBuilder`,
`ListBuilder`) of the format, plus schema/reflection helpers
(`Schema`, `Struct`, `Field`, `StructBuilder`).

Import path: `github.com/zap-proto/go`. Package name: `zap`.

## What this is NOT

- Not a network library. Listeners, connections, transport selection,
  service discovery — all live downstream (e.g. `luxfi/zap`).
- Not Lux-specific. No `luxfi/*` imports, no EVM types, no
  consensus/handshake/PQ-TLS machinery, no mDNS, no QUIC. Those all
  live in `luxfi/zap` and depend on this runtime.
- Not a code generator. `cmd/zapgen` (forthcoming) will live here
  and emit Go from `.zap` schemas — but is not part of this initial
  skeleton.

## Where the spec lives

`github.com/zap-proto/zap-spec` — the wire format spec, magic
constants, version policy. This Go runtime is one of N language
runtimes implementing that spec.

## Where the codegen lives

`github.com/zap-proto/go/cmd/zapgen` (not yet released). Schema
definitions in `schema.go` describe the runtime reflection model
that the generator targets.

## Layout

```
go.mod
zap.go         Message, Object, List, Parse, Root — read side
builder.go     Builder, ObjectBuilder, ListBuilder — write side
schema.go      Type, Struct, Field, Schema, StructBuilder — reflection
*_test.go      Unit tests, fuzzers, benchmarks
examples/      Self-contained demos using only the runtime
```

## Build & test

```bash
go build ./...
go test ./...
```

Both must pass clean — no skipped tests, no expected failures.

## Runtime consolidation with luxfi/zap

`github.com/luxfi/zap` is the network runtime (Node mesh, QUIC/TCP transport,
mDNS, PQ-TLS handshake, EVM types, the `forward` HTTP-over-ZAP contract). It
historically carried its OWN copy of the serialization core (`zap.go`,
`builder.go`, `schema.go`) implementing this exact wire. That duplication is
being collapsed so there is ONE runtime; this package is the canonical core
luxfi/zap converges onto.

### Wire equivalence — proven, pinned

The two cores emit a byte-identical data segment. The proof is split across
both repos and meets at one shared constant, `goldenV1Hex`, pinned verbatim
in `zap_crosswire_test.go` here AND in luxfi/zap:

- this repo's `NewBuilder` emits `goldenV1Hex`; this reader decodes it.
- luxfi/zap's `NewBuilderV1` emits the SAME `goldenV1Hex` (byte-for-byte).

Changing the wire on either side without the other fails CI in both repos.
A live "encode here, decode there" cross-check (a throwaway `go.work` joining
both modules) additionally confirmed every field round-trips both directions.

### The exact deltas this package absorbed (so the two cores AGREE)

Before consolidation this core diverged from luxfi/zap in two ways. Both are
now reconciled here (additively — no honest wire changed):

1. **Version acceptance.** luxfi/zap defines `Version1=1` and `Version2=2`
   (the v2 header carries the v3 platformvm TxKind discriminator at struct
   byte 0; the data segment past magic+version is identical). This reader now
   accepts BOTH versions; before it accepted only `1` and returned
   `ErrInvalidVersion` on a luxfi-default (v2) buffer. `Version` (the bare
   default this Builder emits) stays `Version1` — this is the pure baseline;
   luxfi/zap's default `NewBuilder` emits `Version2`. The ONLY header
   difference between a v1 and a v2 buffer carrying the same payload is byte 4.

2. **Reader hardening (fail-secure).** The accessors now reject malformed
   pointers instead of following them:
   - `Bytes` treats `relOffset` as an UNSIGNED forward pointer and rejects any
     target landing inside the wire header (`absPos < HeaderSize`). The old
     signed cast let a crafted backward pointer alias the header and leak the
     version field (proven by `TestCrossWireRejectsBackwardPointer`).
   - `Object` / `List` reject `absOffset < HeaderSize` (honest nested objects
     live at offset ≥ HeaderSize; the builder never places one in the header).
   - `List` clamps `length ≤ len(data)` to kill the `length=0xFFFFFFFF`
     iterate-4G-times DoS while every per-element accessor still re-checks.

   These match luxfi/zap's hardened reader exactly, so the two readers now
   agree on accept/reject for EVERY input — honest and adversarial.

### Remaining migration steps (toward luxfi/zap riding THIS core)

1. **`schema.go` is already byte-identical** in both repos — the cleanest
   first alias target. luxfi/zap can re-export it (`type Type = zap.Type`, …)
   once it takes a dependency on this module.
2. **Reader/Builder alias.** With the deltas above reconciled, luxfi/zap's
   `zap.go`/`builder.go` are a hardened SUPERSET of this core: luxfi adds
   `ParseHeader`, `WrapBuffer`, `RootObjectAt`, `Object.Offset/Message`,
   `BytesFixedSlice`, `SetBytesFixed`, `ReserveFixed`, `ListStride`, and the
   `Message.refs`/`Release`/`Retain` read-buffer pool. The pool is genuinely
   network-local (tied to the TCP dispatch read path) and STAYS in luxfi/zap;
   the rest can move here so luxfi composes over this core rather than copying
   it. Do this once `cmd/zapgen` lands (it targets THIS reflection model).
3. **`StartObject` reserve discipline (known delta, NOT yet unified).** This
   core's `StartObject` pre-reserves the full fixed section up front
   (`ensureField(dataSize)`); luxfi/zap's does not (it exposes `ReserveFixed`
   for callers and reserves in `Finish`). For every honest layout that writes
   fixed scalars + text/bytes tails (e.g. luxfi's `forward` envelopes) the
   output is byte-identical — proven. The two diverge ONLY for the pathological
   "list/bytes pointer at a LOW offset + a fixed inline field at a HIGHER
   offset whose write is deferred past the tail." Unify by adopting the
   pre-reserve in luxfi/zap (safe — strictly more reserving) when the builder
   is aliased; until then this is a documented, test-covered delta, not a
   silent one.
