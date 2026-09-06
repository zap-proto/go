// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pchain

import (
	"fmt"
	"strings"
)

// Digest renders every field the schema declares into one line of text.
// The Rust side of the proof renders the same line from the same bytes; a
// character of disagreement is a disagreement about the wire.
//
// It lives in the generated code's package because the Go backend keeps a
// view's object and its offsets unexported. (The Rust backend exports both,
// so its half of this proof sits in a separate module.) Nothing here states
// a stride any more: a list answers its own element type, and how wide that
// element is was settled by the schema.

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
		o := outs.At(i)
		fmt.Fprintf(&w, "%d:asset=%x,slock=%d,amt=%d,thr=%d,olock=%d,astart=%d,acount=%d;",
			i, o.Asset(), o.StakeLock(), o.Amount(), o.Threshold(), o.OwnerLock(), o.AddrStart(), o.AddrCount())
	}
	w.WriteString("]")

	addrs := t.OwnerAddrs()
	fmt.Fprintf(&w, ";addrs=%d[", addrs.Len())
	for i := 0; i < addrs.Len(); i++ {
		a := addrs.At(i)
		fmt.Fprintf(&w, "%d:%x;", i, a.Bytes())
	}
	w.WriteString("]")

	ins := t.Ins()
	fmt.Fprintf(&w, ";ins=%d[", ins.Len())
	for i := 0; i < ins.Len(); i++ {
		in := ins.At(i)
		fmt.Fprintf(&w, "%d:txid=%x,idx=%d,asset=%x,slock=%d,amt=%d,sstart=%d,scount=%d;",
			i, in.TxID(), in.OutputIndex(), in.Asset(), in.StakeLock(), in.Amount(), in.SigStart(), in.SigCount())
	}
	w.WriteString("]")

	sigs := t.SigIndices()
	fmt.Fprintf(&w, ";sigs=%d[", sigs.Len())
	for i := 0; i < sigs.Len(); i++ {
		s := sigs.At(i)
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
		s := lens.At(i)
		fmt.Fprintf(&w, "%d:%d;", i, s.Index())
	}
	w.WriteString("]")
	return w.String(), nil
}

// Rebuild writes a transaction back out through the builder its KIND names.
// A P transaction opens with the spending envelope every kind shares and then
// carries its own fields; the schema states the envelope, and two kinds in
// full, so the two are written whole and the rest are written as far as the
// schema goes.
func Rebuild(b []byte) ([]byte, error) {
	t, err := WrapSpend(b)
	if err != nil {
		return nil, err
	}
	switch t.Kind() {
	case kindImport:
		return rebuildImport(b)
	case kindExport:
		return rebuildExport(b)
	}
	return RebuildSpend(b)
}

// The two kinds the schema states in full. The numbers are the chain's, read
// off byte 0 of the transaction.
const (
	kindImport = 4
	kindExport = 5
)

func rebuildImport(b []byte) ([]byte, error) {
	t, err := WrapImport(b)
	if err != nil {
		return nil, err
	}
	return NewImport(ImportInput{
		Kind:         t.Kind(),
		NetworkID:    t.NetworkID(),
		BlockchainID: t.BlockchainID(),
		Outs:         outRecords(t.Outs()),
		OwnerAddrs:   addrRecords(t.OwnerAddrs()),
		Ins:          inRecords(t.Ins()),
		SigIndices:   sigRecords(t.SigIndices()),
		Memo:         t.Memo(),
		SourceChain:  t.SourceChain(),
		ImportedIns:  inRecords(t.ImportedIns()),
		ImportedSigs: sigRecords(t.ImportedSigs()),
	}), nil
}

func rebuildExport(b []byte) ([]byte, error) {
	t, err := WrapExport(b)
	if err != nil {
		return nil, err
	}
	return NewExport(ExportInput{
		Kind:          t.Kind(),
		NetworkID:     t.NetworkID(),
		BlockchainID:  t.BlockchainID(),
		Outs:          outRecords(t.Outs()),
		OwnerAddrs:    addrRecords(t.OwnerAddrs()),
		Ins:           inRecords(t.Ins()),
		SigIndices:    sigRecords(t.SigIndices()),
		Memo:          t.Memo(),
		DestChain:     t.DestChain(),
		ExportedOuts:  outRecords(t.ExportedOuts()),
		ExportedAddrs: addrRecords(t.ExportedAddrs()),
	}), nil
}

// RebuildSpend writes the shared envelope back out through the generated
// builder, carrying every field the builder can carry. The list elements go
// back as the raw record bytes they were read from.
//
// The bytes ARE the vector's bytes, for a vector the schema states in full:
// same version, same order, same strides. That equality is what turns the
// hand-written P wire into something to delete rather than to port.
func RebuildSpend(b []byte) ([]byte, error) {
	t, err := WrapSpend(b)
	if err != nil {
		return nil, err
	}
	return NewSpend(SpendInput{
		Kind:         t.Kind(),
		NetworkID:    t.NetworkID(),
		BlockchainID: t.BlockchainID(),
		Outs:         outRecords(t.Outs()),
		OwnerAddrs:   addrRecords(t.OwnerAddrs()),
		Ins:          inRecords(t.Ins()),
		SigIndices:   sigRecords(t.SigIndices()),
		Memo:         t.Memo(),
	}), nil
}

// The four record runs, each asking its own list for its own elements. No
// stride is named here: the element answers its bytes because it knows how
// wide it is.
func outRecords(l OutList) [][]byte {
	out := make([][]byte, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		out = append(out, l.At(i).Record())
	}
	return out
}

func addrRecords(l AddrList) [][]byte {
	out := make([][]byte, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		out = append(out, l.At(i).Record())
	}
	return out
}

func inRecords(l InList) [][]byte {
	out := make([][]byte, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		out = append(out, l.At(i).Record())
	}
	return out
}

func sigRecords(l SigList) [][]byte {
	out := make([][]byte, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		out = append(out, l.At(i).Record())
	}
	return out
}
