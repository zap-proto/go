# zap-proto/go

## What this is

The canonical Go runtime for the ZAP wire format. Pure stdlib, zero
external dependencies. Provides the read side (`Parse`, `Message`,
`Object`, `List`) and the write side (`Builder`, `ObjectBuilder`,
`ListBuilder`) of the format, plus schema/reflection helpers
(`Schema`, `Struct`, `Field`, `StructBuilder`).

Import path: `github.com/zap-proto/go`. Package name: `zap`.

Three sibling packages ship alongside the root codec:

- `cmd/zapgen` — the schema compiler. Emits per-struct zero-copy
  View/Builder code AND, for every `interface` declaration, a typed RPC
  client + an abstract ordinal-dispatch server contract + a 1-based
  method-ordinal table. Brace and whitespace-significant DSL, one parser.
  `-lang go` (default) emits Go against this runtime; `-lang cpp` emits
  headers against `github.com/zap-proto/cpp`. One front end, one schema
  model, one emitter per language.
- `rpc` — the ZAP call envelope (`BuildRequest`/`ParseRequest`,
  `BuildResponse`/`ParseResponse`, `Call`, `Response`, status codes). The
  wire contract the generated client/server ride; byte-compatible with the
  other language runtimes' transport envelopes (Version2 header, router
  msgType flags).
- `cap` — the capability runtime (Issue/Attenuate/Verify/VerifyChain/
  Revoke). Signature scope is SPEC §3 canonical bytes
  (`Capability[0..164) || canonical(Caveats)`, see `cap.CanonicalBytes`);
  the delegation gate (SPEC §2.3 step 3d), scheme-aware fail-closed
  signature dispatch, and the cross-cutting Permission bits are enforced.

## What this is NOT

- Not a network library. Listeners, connections, transport selection,
  service discovery — all live downstream (e.g. `luxfi/zap`). The `rpc`
  package defines the call ENVELOPE (the bytes), not the transport that
  carries it: the generated client takes a `Channel` the consumer
  supplies, and the generated server is dispatched by the consumer's
  read loop. Sockets, framing, and handshakes stay downstream.
- Not Lux-specific. No `luxfi/*` imports, no EVM types, no
  consensus/handshake/PQ-TLS machinery, no mDNS, no QUIC. Those all
  live in `luxfi/zap` and depend on this runtime.

## Where the spec lives

`github.com/zap-proto/zap-spec` — the wire format spec, magic
constants, version policy. This Go runtime is one of N language
runtimes implementing that spec.

## Where the codegen lives

`github.com/zap-proto/go/cmd/zapgen` — shipped and tested. It parses
`.zap` schemas (brace + whitespace forms, one parser via `desugar.go`)
and emits, per struct, a zero-copy View and a Builder, and per
`interface`, a typed RPC client + abstract dispatch server + 1-based
ordinal table over the `rpc` envelope. Drop a `//go:generate zapgen
schema.zap` line in the consuming package; `examples/echo` is a worked
end-to-end demo (generated code + in-memory client/server round-trip
test).

### The read side is total

An out-of-range read answers zero rather than faulting, which is what lets a
hostile buffer go straight to a typed accessor with no validation pass in
front of it. That has to hold for the absent `Object` too — a null pointer
field resolves to it, and a generated accessor returns it BY VALUE, so a
caller has no way to test for it before reading. `Object.buf()` in `zap.go`
is where that is true; before it, exactly the case a hostile or truncated
buffer steers a reader to was the one that panicked.

### One front end, three backends

`-lang go` (the default), `-lang rust`, `-lang cpp`. The parser, the desugar
and the schema model are shared; only the emitter differs — `emit.go`,
`emit_rust.go`, `emitcpp.go`. A backend that grew its own parser would be the
thing this generator exists to remove, and `TestOneFrontEnd` says so.

| `-lang` | output | runtime it calls |
|---------|--------|------------------|
| `go`    | `<struct>_zap.go`  | `github.com/zap-proto/go` |
| `rust`  | `<schema>_zap.rs`  | `zap.rs`, written beside it |
| `cpp`   | `<struct>_zap.hpp` | `github.com/zap-proto/cpp` |

Rust compiles by module, not by directory, so a schema emits ONE
`<schema>_zap.rs` (there is no per-struct form; `-single` is implied),
plus the runtime it calls: `zap.rs`, and `rpc.rs` when the schema
declares an interface. Those two are the only hand-maintained Rust in
the toolchain; they live in `cmd/zapgen/rust/`, are embedded in the
binary, and are written out verbatim beside the module. Fix a runtime
bug there and every Rust consumer gets it at once.

```bash
zapgen -lang rust -out ./src schema.zap        # module + runtime
zapgen -lang rust -rust-runtime crate::wire … # runtime filed elsewhere
```

The emitted Rust reads by borrowing: `Foo<'a>` wraps a `zap::Object<'a>`,
a field is a bounds-checked look at bytes already in hand, and
`bytes`/`text`/`bytes_fixed` answer `&'a [u8]` / `&'a str` / `&'a [u8; N]`
into the caller's buffer. Nothing is decoded into an owned struct and
there is no encoder, decoder or codec: the builder writes into one
buffer and hands it over. Offsets and a from-object constructor are
`pub`, so a list element can be read as a typed view from outside the
generated module — the Go backend keeps both unexported, which is the
one asymmetry between them.

Two places the C++ emitter differs because C++ does:

- A nested-struct accessor is written under its qualified name
  (`::pkg::Child`), because a field may carry the name of its own type and the
  member would otherwise shadow the class. Go has no such collision.
- Two structs that point at each other need `-single`. Per-struct headers
  cannot both be complete for the other, so the cycle is expressible in one
  header and not in two. Go compiles either.

And one thing every backend spells rather than defaults: **a
`bytes_fixed[N]` field is always N bytes.** Go's `[N]byte` answers N zeros
for a buffer too short to hold it, so the C++ span accessor answers a zero
span of length N rather than an empty one.

### The proof that the backends agree

`conformance/` — not a unit test, a differential. Every backend is
generated from the same schemas; the Go program and its Rust and C++ twins
read the same corpus, run the same fields through the emitted code, and
write the same report. The reports are compared byte for byte.

```bash
sh conformance/run.sh
```

The corpus is `conformance/corpus/vectors.tsv`, a verbatim copy of
node2's `conformance/corpus/vectors.tsv`: 208 P-chain and X-chain
vectors written by the Go node. 127 parse, 77 are refused (every one a
deliberate truncation or edge case), 4 carry no wire bytes. Each parsed
vector is read field by field, written back out through the emitted
builder, and read again. `conformance/schema/` states the P and X wire
as schemas — offsets taken from node2's hand-written Rust — and the
emitted sizes come out equal to the strides that code states by hand
(Out 72, In 96, Addr 20, Sig 4, Spend 77, P block 73, X block 96).

`conformance/schema/kitchen.zap` carries every type the dialect has, so
the build side is exercised beyond the handful a chain happens to use.

### Schema syntax — two equivalent forms, one parser

`.zap` schemas may be written brace-style or whitespace-significant; both
compile to the same Go. The brace form is canonical and unchanged:

```
struct BaseTx {
    NetworkID    u32   @0
    Memo         bytes @52
}
```

The whitespace form drops braces (blocks open by indentation) and may
drop `@N` byte-offsets (auto-assigned by accumulating each type's slot
width — `Type.SlotSize()`, the single layout authority):

```
struct BaseTx
    NetworkID u32
    Memo      bytes
```

An `interface` declares an RPC service. Method ordinals auto-assign
`1, 2, 3, …` in declaration order (appending never renumbers), each
method takes at most one struct request and, after `returns`, at most one
struct response (either may be empty). `interface` opens a whitespace
block too — its method lines pass through verbatim (they carry no `@N`):

```
interface Echo {
    ping(req: Ping) returns (resp: Pong)
    notify(req: Ping)
    health() returns (resp: Pong)
    shutdown()
}
```

`cmd/zapgen/desugar.go` (`Desugar(src) -> braceSrc`) runs BEFORE the
tokenizer in `Parse`, rewriting the whitespace form into brace source.
Invariant: a pure-brace file (every header ends `{`, every field carries
`@N`) round-trips byte-for-byte, so the proven brace parser is untouched
and styles may mix per top-level decl. An explicit `@N` is always
preserved and resets the offset cursor. Proven by
`TestWhitespaceEquivalence` (brace fixture == whitespace twin, identical
generated Go) and `TestGolden` (brace regression). The whitespace twin
fixtures live under `cmd/zapgen/testdata/ws/`.

## Promise pipelining — the ONE canonical model (Target-based)

The call envelope (`rpc/envelope.go`) has always carried a `Target u32` at
struct offset 8. That field IS the pipelining mechanism, and `rpc/pipeline.go`
makes it real on both sides:

- A call carries a caller-assigned `PromiseID` (the id its answer resolves
  to). A **dependent** call sets `Target = a prior call's PromiseID`, meaning
  "before you dispatch me, substitute the resolved Body of the call that
  answered to that PromiseID as my Payload." The result of A is the input to
  B, so B ships back-to-back with A — no round trip threads A's answer back
  through the client first.
- **Client side: `rpc.Session`** allocates unique non-zero PromiseIDs (1, 2,
  …) and builds Calls: `Origin(p, …)` (Target = `NoTarget`) and
  `Pipeline(p, target, …)` (Target = `target.ID`).
- **Server side: `rpc.Pipeliner`** wraps a `Dispatch<Iface>` and is the
  promise table for one connection: it records every OK answer under its
  PromiseID, resolves a dependent's `Target` before dispatch, **queues** a
  dependent whose Target has not resolved yet until it does, and **refuses**
  (`StatusBadRequest`) a dependent whose Target answered non-OK or was
  `Finish`ed (so it never hangs). `Finish(id)` bounds the table (the analogue
  of capnp's Finish).

zapgen emits this directly: the generated `Client` holds a `*rpc.Session`,
each method `M` is the originating call (Target = `NoTarget`), and `MOn(on
rpc.Promise)` is the dependent form (Target = `on.ID`, payload supplied
server-side from `on`'s answer). Both return the call's own `rpc.Promise` so
chains compose. **`NoTarget` is no longer hardcoded** — it is the value the
plain form passes through the one shared `invoke<M>(target, payload)` path.

Proven end-to-end by `rpc/pipeline_test.go` (auth→getResource): Target
resolution in one pass, server-side queuing when the dependent arrives first,
non-OK and Finished targets refused not hung, and the Target riding on the
wire byte-for-byte. The TS runtime mirrors this exactly (`@zap-proto/zap`
`Session`/`Pipeliner` in `js/src/promise.ts`), so a pipeline built on one
runtime resolves on the other; a non-pipelining peer interoperates by sending
`Target = NoTarget`.

**Boundary with the Rust stack.** `rust/zap-rpc` implements the full capnp
Level-1 RPC model: `PromisedAnswer` with a transform *path* of pointer/struct
ops and an `ImportedCap` union — a strict superset that can pipeline on a
*field of* an answer, not just the whole answer. That is a richer RPC layer,
not a competing envelope. The Go/TS Target model and the Rust capnp model
interoperate at the envelope level for non-pipelined calls (`Target =
NoTarget`); unifying the capnp transform path into the Go envelope is a
separate, deliberate superset and is NOT claimed here.

## Layout

```
go.mod
zap.go         Message, Object, List, Parse, Root — read side
builder.go     Builder, ObjectBuilder, ListBuilder — write side
schema.go      Type, Struct, Field, Schema, StructBuilder — reflection
rpc/           Call envelope (BuildRequest/ParseRequest/Build/ParseResponse)
               + promise pipelining (Session, Pipeliner) — pipeline.go
cap/           Capability runtime: Issue/Attenuate/Verify/VerifyChain/Revoke
cmd/zapgen/    Schema compiler: one front end (parser + desugar), a
               backend per language (emit.go, emit_rust.go, emitcpp.go),
               and the Rust runtime it emits verbatim (rust/zap.rs,
               rust/rpc.rs)
conformance/   The cross-language proof: same schemas, same corpus, one
               report, compared byte for byte (run.sh)
*_test.go      Unit tests, fuzzers, benchmarks
examples/      Self-contained demos (agents mesh; echo RPC service)
```

## Build & test

```bash
go build ./...
go test ./...
sh conformance/run.sh   # every backend must agree, byte for byte
```

All three must pass clean — no skipped tests, no expected failures. The
proof needs a Rust toolchain and a C++23 compiler; nothing else in the
repo does.

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
