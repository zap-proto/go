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
