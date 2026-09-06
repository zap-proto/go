# Three backends, one wire

`zapgen` emits Go, Rust and C++ from the same schema. This directory is the
evidence that the three agree, and it is run, not asserted:

```
sh conformance/run.sh
```

It needs a Go toolchain, a Rust toolchain and a C++23 compiler. The C++
runtime (`github.com/zap-proto/cpp`) is cloned at the revision `run.sh` pins,
unless `ZAP_CPP` names a checkout.

## What it compares

1. **The report, byte for byte.** Each language builds the same messages from
   the same inputs, reads the same corpus with the code zapgen emitted for it,
   and writes one line per case. Three files, `cmp`d. A character of
   disagreement is a disagreement about the wire, and the diff says where.

2. **The chain's own bytes.** Every P transaction and X envelope in the corpus
   is read, written back out through the emitted builder, and the result is
   compared to what the Go node wrote. Where the schema states the whole
   transaction the two are the same bytes — same version, same order, same
   strides. Where it states only the shared envelope the rebuilt message is
   shorter, and the report says so by size. **No vector differs in a byte**,
   and run.sh fails if one ever does.

3. **The controls.** A proof that cannot fail proves nothing, so
   `control.sh` breaks one thing at a time and checks the proof notices: one
   language stamping the other wire version, one reading two fields at each
   other's offsets, one reading a record list at the wrong width, one shipping
   a different capability byte — and, last, every language told the same wrong
   record width. That one cannot break the three-way comparison (three
   backends told the same wrong thing agree perfectly) and it is why the
   corpus is here: the vectors were written by the chain, and a stride the
   chain does not use stops matching them.

## The parts

```
schema/         the P and X wire as schemas, offsets from node2's chains,
                plus kitchen.zap — every type the dialect has, in one struct
corpus/         node2's vectors.tsv, verbatim: 208 P and X vectors written
                by the Go node, 77 of them damaged on purpose
main.go         the Go half; pchain/ xchain/ kitchen/ echo/ hold what zapgen
                emitted plus the digest that renders it
rust/           the Rust half, same shape
cpp/probe.cpp   the C++ half, same shape
run.sh          regenerate, build, run, compare
control.sh      break it five ways and check the proof notices
```

The generated files are committed so a reader can see what the generator
says without running it; `run.sh` rewrites them first, so a stale one cannot
pass.
