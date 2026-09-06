#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause-Eco
# Regenerate both backends from the schemas, run both halves of the proof,
# and compare the two reports byte for byte.
#
# Run from the repository root:  sh conformance/run.sh
set -e

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
gen="go run ./cmd/zapgen"

echo "== generating Go"
$gen -single -out conformance/pchain  conformance/schema/pchain.zap
$gen -single -out conformance/xchain  conformance/schema/xchain.zap
$gen -single -out conformance/kitchen conformance/schema/kitchen.zap
$gen -single -out conformance/echo    cmd/zapgen/testdata/echo.zap

echo "== generating Rust"
$gen -lang rust -out conformance/rust/src conformance/schema/pchain.zap
$gen -lang rust -out conformance/rust/src conformance/schema/xchain.zap
$gen -lang rust -out conformance/rust/src conformance/schema/kitchen.zap
$gen -lang rust -out conformance/rust/src cmd/zapgen/testdata/echo.zap

echo "== go"
go build ./...
go run ./conformance > "$out/go.tsv"

echo "== rust"
# Cargo decides what to rebuild from mtimes at one-second granularity, so a
# regeneration landing in the same second as the last build can be skipped —
# and a proof that silently runs the previous binary is worse than no proof.
# The crate is small; drop it and build it.
cargo clean --quiet -p zapproof --manifest-path conformance/rust/Cargo.toml
cargo build --quiet --manifest-path conformance/rust/Cargo.toml
cargo run --quiet --manifest-path conformance/rust/Cargo.toml -- -corpus conformance/corpus/vectors.tsv > "$out/rust.tsv"

echo "== compare"
if cmp "$out/go.tsv" "$out/rust.tsv"; then
	echo "IDENTICAL: $(wc -l < "$out/go.tsv") lines"
else
	echo "DIFFER"
	exit 1
fi
