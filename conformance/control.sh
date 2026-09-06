#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# The controls. A proof that cannot fail proves nothing, so each of these
# damages exactly one thing and the report has to notice.
#
#   header      one language stamps the other wire version
#   field       one language reads two fields at each other's offsets
#   stride      one language reads a record list at the wrong width
#   capability  one language ships a different capability byte
#   width       EVERY language is told a record is four bytes narrower
#
# The first four must break the three-way comparison. The last one cannot —
# three backends told the same wrong thing agree with each other perfectly —
# and it is the reason the corpus is in the proof at all: the vectors were
# written by the chain, and a stride the chain does not use stops matching
# them. A control nobody can fail is a control nobody is running.
#
# Called by run.sh:  control.sh WORKDIR CORPUS ZAP_CPP HPP
set -e

out=$1
corpus=$2
zapcpp=$3
hpp=$4
tree=$out/tree
fail=0

mkdir -p "$tree"
tar -cf - --exclude=./conformance/rust/target --exclude=./.git . | (cd "$tree" && tar -xf -)

# says NAME differed-or-not, against what the healthy run wrote
verdict() {
	if [ "$2" = "changed" ]; then
		echo "  $1: caught"
	else
		echo "  $1: NOT CAUGHT — the proof did not notice"
		fail=1
	fi
}

changed() { # changed FILE BASELINE
	if cmp -s "$1" "$2"; then echo unchanged; else echo changed; fi
}

# ── header: the Rust builder stamps version 1 ──────────────────────────────
sed -i 's/Builder::new_v2(256)/Builder::new(256)/' "$tree"/conformance/rust/src/*_zap.rs
(cd "$tree" && cargo run --quiet --manifest-path conformance/rust/Cargo.toml -- \
	-corpus "$corpus" > "$out/ctl-header.tsv" 2>/dev/null) || true
verdict header "$(changed "$out/ctl-header.tsv" "$out/go.tsv")"

# ── field: two Go offsets swap places ──────────────────────────────────────
go_pchain=$tree/conformance/pchain/pchain_zap.go
sed -i 's/spendOutsOff  *= 37/spendOutsOff = 53/; s/spendInsOff  *= 53/spendInsOff = 37/' "$go_pchain"
(cd "$tree" && go run ./conformance -corpus "$corpus" > "$out/ctl-field.tsv" 2>/dev/null) || true
verdict field "$(changed "$out/ctl-field.tsv" "$out/go.tsv")"

# ── stride: the C++ reader is told a record is wider than it is ────────────
ctl_hpp=$out/ctl-hpp
rm -rf "$ctl_hpp" && cp -r "$hpp" "$ctl_hpp"
# The width the ELEMENT accessor uses, which is where a wrong stride shifts
# every record. (The width the list is BOUNDED by is a second use of the same
# number; damaging that one only changes which counts are refused.)
sed -i 's/l_.object(i, kOutSize)/l_.object(i, kOutSize + 8)/' "$ctl_hpp/pchain_zap.hpp"
${CXX:-g++} -std=c++23 -O1 -I"$zapcpp/include" -I"$ctl_hpp" \
	-o "$out/ctl-probe-cpp" conformance/cpp/probe.cpp "$zapcpp/src/rpc.cpp"
"$out/ctl-probe-cpp" -corpus "$corpus" > "$out/ctl-stride.tsv" || true
verdict stride "$(changed "$out/ctl-stride.tsv" "$out/go.tsv")"

# ── capability: one byte of the cap the Go client ships ────────────────────
sed -i 's/0xca, 0xfe/0xca, 0xff/' "$tree/conformance/main.go"
(cd "$tree" && go run ./conformance -corpus "$corpus" > "$out/ctl-cap.tsv" 2>/dev/null) || true
verdict capability "$(changed "$out/ctl-cap.tsv" "$out/go.tsv")"

# ── width: every language is told the record is 68 bytes, not 72 ───────────
# The three still agree with each other. What stops agreeing is the chain.
wide=$out/wide
rm -rf "$wide" && mkdir -p "$wide"
tar -cf - --exclude=./conformance/rust/target --exclude=./.git . | (cd "$wide" && tar -xf -)
sed -i '/^    Pad       bytes_fixed\[4\] @68$/d' "$wide/conformance/schema/pchain.zap"
(cd "$wide" && go run ./cmd/zapgen -single -out conformance/pchain conformance/schema/pchain.zap &&
	go run ./conformance -corpus "$corpus" > "$out/ctl-width.tsv" 2>/dev/null) || true
# Counted over the P vectors alone: the narrowed record is a P record, and an
# X vector that still matches says nothing either way.
before=$(grep '^EQ	P_' "$out/go.tsv" | grep -c 'yes$' || true)
after=$(grep '^EQ	P_' "$out/ctl-width.tsv" | grep -c 'yes$' || true)
if [ "$before" -gt 0 ] && [ "$after" -lt "$before" ]; then
	echo "  width: caught — $before P vectors matched the chain, $after with the wrong stride"
else
	echo "  width: NOT CAUGHT — $before P vectors before, $after after"
	fail=1
fi

exit $fail
