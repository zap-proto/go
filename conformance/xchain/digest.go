// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xchain

import (
	"fmt"
	"strings"
)

// Prefix is the two bytes an X vector carries ahead of its ZAP message: a
// type byte and a shape byte. The framing is the chain's, so stripping it is
// the caller's job, not the runtime's.
const Prefix = 2

// SignedOf renders the outer envelope of an X vector, and the spending
// envelope nested inside its signed bytes.
func SignedOf(b []byte) (string, error) {
	if len(b) < Prefix {
		return "", fmt.Errorf("short envelope")
	}
	t, err := WrapSigned(b[Prefix:])
	if err != nil {
		return "", err
	}
	var w strings.Builder
	fmt.Fprintf(&w, "type=%d;shape=%d;unsigned=%d;creds=%d;credbytes=%d",
		b[0], b[1], len(t.Unsigned()), t.CredentialCount(), len(t.CredentialBytes()))

	inner := t.Unsigned()
	if len(inner) > Prefix {
		if base, err := WrapBase(inner[Prefix:]); err == nil {
			fmt.Fprintf(&w, ";base{itype=%d;ishape=%d;net=%d;chain=%x;memo=%x;outs=%d[",
				inner[0], inner[1], base.NetworkID(), base.BlockchainID(), base.Memo(), base.Outs().Len())
			outs := base.Outs()
			for i := 0; i < outs.Len(); i++ {
				p := outs.At(i)
				fmt.Fprintf(&w, "%d:%d;", i, p.Offset())
			}
			ins := base.Ins()
			fmt.Fprintf(&w, "];ins=%d[", ins.Len())
			for i := 0; i < ins.Len(); i++ {
				p := ins.At(i)
				fmt.Fprintf(&w, "%d:%d;", i, p.Offset())
			}
			w.WriteString("]}")
		} else {
			fmt.Fprintf(&w, ";base{unreadable}")
		}
	} else {
		fmt.Fprintf(&w, ";base{absent}")
	}
	return w.String(), nil
}

// RebuildSigned writes the outer envelope back out through the generated
// builder.
func RebuildSigned(b []byte) ([]byte, error) {
	if len(b) < Prefix {
		return nil, fmt.Errorf("short envelope")
	}
	t, err := WrapSigned(b[Prefix:])
	if err != nil {
		return nil, err
	}
	return NewSigned(SignedInput{
		Unsigned:        t.Unsigned(),
		CredentialCount: t.CredentialCount(),
		CredentialBytes: t.CredentialBytes(),
	}), nil
}

// BlockOf renders an X block. Unlike a transaction it carries no framing
// bytes ahead of the message.
func BlockOf(b []byte) (string, error) {
	t, err := WrapBlock(b)
	if err != nil {
		return "", err
	}
	var w strings.Builder
	fmt.Fprintf(&w, "parent=%x;height=%d;time=%d;root=%x;blob=%x",
		t.Parent(), t.Height(), t.Time(), t.Root(), t.TxBlob())
	lens := t.TxLengths()
	fmt.Fprintf(&w, ";txlens=%d[", lens.Len())
	for i := 0; i < lens.Len(); i++ {
		p := lens.At(i)
		fmt.Fprintf(&w, "%d:%d;", i, p.Offset())
	}
	w.WriteString("]")
	return w.String(), nil
}
