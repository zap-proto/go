#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause-Eco
# Regenerate all three backends from the same schemas, run all three halves of
# the proof, and compare the three reports byte for byte. Then damage the
# wire four ways and check the proof says so.
#
# Run from the repository root:  sh conformance/run.sh
#
# Needs a Go toolchain, a Rust toolchain and a C++23 compiler. The C++ runtime
# comes from github.com/zap-proto/cpp, pinned below; set ZAP_CPP to a checkout
# to use one you already have.
set -e

ZAP_CPP_REV=c75f8998c0701555e4b6fa95b6af0fd46f48f6ab
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
gen="go run ./cmd/zapgen"
corpus=conformance/corpus/vectors.tsv

# ── the C++ runtime ────────────────────────────────────────────────────────
if [ -z "${ZAP_CPP:-}" ]; then
	ZAP_CPP=$out/zap-cpp
	git clone -q https://github.com/zap-proto/cpp "$ZAP_CPP"
	git -C "$ZAP_CPP" checkout -q $ZAP_CPP_REV
fi
echo "runtime: $ZAP_CPP @ $(git -C "$ZAP_CPP" rev-parse --short HEAD)"

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

echo "== generating C++"
hpp=$out/hpp
mkdir -p "$hpp"
$gen -lang cpp -single -out "$hpp" conformance/schema/pchain.zap
$gen -lang cpp -single -out "$hpp" conformance/schema/xchain.zap
$gen -lang cpp -single -out "$hpp" conformance/schema/kitchen.zap
$gen -lang cpp -single -out "$hpp" cmd/zapgen/testdata/echo.zap

# The per-struct headers are a second output shape, and they have to build
# too: -single is what the probe uses, and one header per struct with a
# sibling include per named struct is the default.
per=$out/per-struct
mkdir -p "$per"
$gen -lang cpp -out "$per" conformance/schema/pchain.zap
$gen -lang cpp -out "$per" conformance/schema/xchain.zap
$gen -lang cpp -out "$per" cmd/zapgen/testdata/echo.zap
{
	for h in "$per"/*.hpp; do echo "#include \"$(basename "$h")\""; done
	echo "int main() { return 0; }"
} > "$per/all.cpp"
${CXX:-g++} -std=c++23 -O1 -Wall -Wextra -I"$ZAP_CPP/include" -I"$per" \
	-o "$out/per-struct-all" "$per/all.cpp" "$ZAP_CPP/src/rpc.cpp"
echo "per-struct headers: $(ls "$per"/*.hpp | wc -l) compiled together"

echo "== go"
go build ./...
go run ./conformance -corpus "$corpus" > "$out/go.tsv"

echo "== rust"
# Cargo decides what to rebuild from mtimes at one-second granularity, so a
# regeneration landing in the same second as the last build can be skipped —
# and a proof that silently runs the previous binary is worse than no proof.
# The crate is small; drop it and build it.
cargo clean --quiet -p zapproof --manifest-path conformance/rust/Cargo.toml
cargo build --quiet --manifest-path conformance/rust/Cargo.toml
cargo run --quiet --manifest-path conformance/rust/Cargo.toml -- -corpus "$corpus" > "$out/rust.tsv"

echo "== cpp"
${CXX:-g++} -std=c++23 -O1 -Wall -Wextra -I"$ZAP_CPP/include" -I"$hpp" \
	-o "$out/probe-cpp" conformance/cpp/probe.cpp "$ZAP_CPP/src/rpc.cpp"
"$out/probe-cpp" -corpus "$corpus" > "$out/cpp.tsv"

echo "== compare"
if cmp "$out/go.tsv" "$out/rust.tsv" && cmp "$out/go.tsv" "$out/cpp.tsv"; then
	echo "IDENTICAL: $(wc -l < "$out/go.tsv") lines, three languages"
else
	echo "DIFFER"
	exit 1
fi

echo "== the chain's own bytes"
eq=$(grep -c '^EQ' "$out/go.tsv")
yes=$(grep '^EQ' "$out/go.tsv" | grep -c 'yes$' || true)
byte=$(grep '^EQ' "$out/go.tsv" | grep -c ';at=' || true)
echo "written back: $eq vectors, $yes byte-identical to the chain, $byte differing in a byte"
if [ "$byte" != "0" ]; then
	echo "A REBUILT VECTOR DISAGREES WITH THE CHAIN IN A BYTE"
	exit 1
fi

# ── the controls ───────────────────────────────────────────────────────────
# A proof that cannot fail proves nothing. Each of these damages one thing and
# the report must change; a control that leaves the report alone is a control
# that was never watching.
echo "== controls"
sh conformance/control.sh "$out" "$corpus" "$ZAP_CPP" "$hpp"
