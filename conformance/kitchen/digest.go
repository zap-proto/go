// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kitchen

import (
	"fmt"
	"strings"
)

// LeafOf renders every field of a Leaf message.
func LeafOf(b []byte) string {
	t, err := WrapLeaf(b)
	if err != nil {
		return "err=" + err.Error()
	}
	return leaf(t)
}

func leaf(t Leaf) string {
	return fmt.Sprintf("tag=%d;note=<%s>", t.Tag(), t.Note())
}

// AllOf renders every field of an All message, including each element of
// its list and the struct nested inside it.
func AllOf(b []byte) string {
	t, err := WrapAll(b)
	if err != nil {
		return "err=" + err.Error()
	}
	var w strings.Builder
	fmt.Fprintf(&w, "flag=%t;a8=%d;a16=%d;a32=%d;a64=%d", t.Flag(), t.A8(), t.A16(), t.A32(), t.A64())
	fmt.Fprintf(&w, ";s8=%d;s16=%d;s32=%d;s64=%d", t.S8(), t.S16(), t.S32(), t.S64())
	// Bit patterns, not printed decimals: a float's text is the one thing
	// two languages are certain to render differently.
	fmt.Fprintf(&w, ";f32=%08x;f64=%016x", f32bits(t.F32()), f64bits(t.F64()))
	fmt.Fprintf(&w, ";name=<%s>;blob=%x;id=%x", t.Name(), t.Blob(), t.Id())

	items := t.Items()
	fmt.Fprintf(&w, ";items=%d[", items.Len())
	for i := 0; i < items.Len(); i++ {
		fmt.Fprintf(&w, "%d:%s;", i, leaf(items.At(i)))
	}
	w.WriteString("]")
	fmt.Fprintf(&w, ";inner=%s", leaf(t.Inner()))
	return w.String()
}
