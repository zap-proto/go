// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pchain

import (
	"fmt"
	"strings"

	zap "github.com/zap-proto/go"
)

// Digest renders every field the schema declares into one line of text.
// The Rust side of the proof renders the same line from the same bytes; a
// character of disagreement is a disagreement about the wire.
//
// It lives in the generated code's package because the Go backend keeps a
// view's object and its offsets unexported — a list element cannot be read
// as a typed view from outside. (The Rust backend exports both, so its half
// of this proof sits in a separate module.)

// Spend renders the transaction at the root of a P vector.
func SpendOf(b []byte) (string, error) {
	t, err := WrapSpend(b)
	if err != nil {
		return "", err
	}
	var w strings.Builder
	fmt.Fprintf(&w, "kind=%d;net=%d;chain=%x;memo=%x", t.Kind(), t.NetworkID(), t.BlockchainID(), t.Memo())

	outs := t.Outs()
	fmt.Fprintf(&w, ";outs=%d[", outs.Len())
	for i := 0; i < outs.Len(); i++ {
		o := Out{o: outs.Object(i, outSize)}
		fmt.Fprintf(&w, "%d:asset=%x,slock=%d,amt=%d,thr=%d,olock=%d,astart=%d,acount=%d;",
			i, o.Asset(), o.StakeLock(), o.Amount(), o.Threshold(), o.OwnerLock(), o.AddrStart(), o.AddrCount())
	}
	w.WriteString("]")

	addrs := t.OwnerAddrs()
	fmt.Fprintf(&w, ";addrs=%d[", addrs.Len())
	for i := 0; i < addrs.Len(); i++ {
		a := Addr{o: addrs.Object(i, addrSize)}
		fmt.Fprintf(&w, "%d:%x;", i, a.Bytes())
	}
	w.WriteString("]")

	ins := t.Ins()
	fmt.Fprintf(&w, ";ins=%d[", ins.Len())
	for i := 0; i < ins.Len(); i++ {
		in := In{o: ins.Object(i, inSize)}
		fmt.Fprintf(&w, "%d:txid=%x,idx=%d,asset=%x,slock=%d,amt=%d,sstart=%d,scount=%d;",
			i, in.TxID(), in.OutputIndex(), in.Asset(), in.StakeLock(), in.Amount(), in.SigStart(), in.SigCount())
	}
	w.WriteString("]")

	sigs := t.SigIndices()
	fmt.Fprintf(&w, ";sigs=%d[", sigs.Len())
	for i := 0; i < sigs.Len(); i++ {
		s := Sig{o: sigs.Object(i, sigSize)}
		fmt.Fprintf(&w, "%d:%d;", i, s.Index())
	}
	w.WriteString("]")
	return w.String(), nil
}

// BlockOf renders the block at the root of a P block vector.
func BlockOf(b []byte) (string, error) {
	t, err := WrapBlock(b)
	if err != nil {
		return "", err
	}
	var w strings.Builder
	fmt.Fprintf(&w, "kind=%d;parent=%x;height=%d;time=%d;blob=%x;proposal=%x",
		t.Kind(), t.Parent(), t.Height(), t.Time(), t.TxBlob(), t.ProposalTx())
	lens := t.TxLengths()
	fmt.Fprintf(&w, ";txlens=%d[", lens.Len())
	for i := 0; i < lens.Len(); i++ {
		s := Sig{o: lens.Object(i, sigSize)}
		fmt.Fprintf(&w, "%d:%d;", i, s.Index())
	}
	w.WriteString("]")
	return w.String(), nil
}

// RebuildSpend writes the transaction back out through the generated
// builder, carrying every field the builder can carry. The list elements go
// back as the raw record bytes they were read from.
//
// The bytes are NOT the vector's bytes: the builder writes a list as
// length-prefixed entries, while the chain writes fixed-stride records. What
// is proven here is that the Go and the Rust builder, handed the same
// values, write the same bytes.
func RebuildSpend(b []byte) ([]byte, error) {
	t, err := WrapSpend(b)
	if err != nil {
		return nil, err
	}
	return NewSpend(SpendInput{
		Kind:         t.Kind(),
		NetworkID:    t.NetworkID(),
		BlockchainID: t.BlockchainID(),
		Outs:         records(t.Outs(), outSize),
		OwnerAddrs:   records(t.OwnerAddrs(), addrSize),
		Ins:          records(t.Ins(), inSize),
		SigIndices:   records(t.SigIndices(), sigSize),
		Memo:         t.Memo(),
	}), nil
}

// records slices a stride list into one byte run per element.
func records(l zap.List, stride int) [][]byte {
	out := make([][]byte, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		o := l.Object(i, stride)
		out = append(out, o.BytesFixed(0, stride))
	}
	return out
}
