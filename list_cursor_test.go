// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// buildVarBytesList encodes vals as a variable-element list (the
// AddObjectBytes shape) and returns a parsed message whose root carries
// the list at field offset 0. List-first discipline: write the list
// before the object so the object's fixed section lands after it.
func buildVarBytesList(t testing.TB, vals [][]byte) List {
	t.Helper()
	b := NewBuilder(256)
	lb := b.StartList(0)
	for _, v := range vals {
		lb.AddObjectBytes(v)
	}
	off, n := lb.Finish()
	ob := b.StartObject(8)
	ob.SetList(0, off, n)
	ob.FinishAsRoot()
	msg, err := Parse(b.Finish())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return msg.Root().List(0)
}

// TestEachBytes_MatchesBytesAt is the equivalence proof: a single forward
// EachBytes pass yields exactly the slices BytesAt(i) yields for every i,
// in order, including empty-payload entries (which must survive as
// non-nil zero-length slices, like a genuine "").
func TestEachBytes_MatchesBytesAt(t *testing.T) {
	want := [][]byte{
		[]byte("self-hosted"),
		[]byte("macos"),
		{}, // empty payload: a genuine "" must survive
		[]byte("arm64"),
		[]byte("a"),
		bytes.Repeat([]byte("x"), 300), // multi-byte length prefix
	}
	l := buildVarBytesList(t, want)

	if l.Len() != len(want) {
		t.Fatalf("Len() = %d, want %d", l.Len(), len(want))
	}

	// Single-pass collect.
	var got [][]byte
	l.EachBytes(func(i int, b []byte) bool {
		if i != len(got) {
			t.Fatalf("EachBytes called out of order: i=%d, expected %d", i, len(got))
		}
		got = append(got, b)
		return true
	})

	if len(got) != len(want) {
		t.Fatalf("EachBytes yielded %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		// Byte-equality with BytesAt(i): the cursor must be a faithful
		// drop-in for indexed access.
		if !bytes.Equal(got[i], l.BytesAt(i)) {
			t.Fatalf("entry %d: EachBytes=%q BytesAt=%q", i, got[i], l.BytesAt(i))
		}
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("entry %d: got %q, want %q", i, got[i], want[i])
		}
		// Empty payload survives as non-nil (matches BytesAt).
		if len(want[i]) == 0 && got[i] == nil {
			t.Fatalf("entry %d: empty payload yielded nil, want non-nil zero-length", i)
		}
	}
}

// TestEachBytes_EarlyStop proves fn returning false halts the walk.
func TestEachBytes_EarlyStop(t *testing.T) {
	l := buildVarBytesList(t, [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")})
	var seen int
	l.EachBytes(func(i int, b []byte) bool {
		seen++
		return i < 1 // stop after index 1
	})
	if seen != 2 {
		t.Fatalf("early-stop visited %d entries, want 2", seen)
	}
}

// TestEach_MatchesObjectAt proves the Object cursor yields the same
// elements as ObjectAt(i), parsing each entry's sub-buffer once.
func TestEach_MatchesObjectAt(t *testing.T) {
	// Three real sub-objects, each a tiny ZAP buffer carrying a uint32.
	mk := func(v uint32) []byte {
		b := NewBuilder(64)
		ob := b.StartObject(4)
		ob.SetUint32(0, v)
		ob.FinishAsRoot()
		return b.Finish()
	}
	l := buildVarBytesList(t, [][]byte{mk(10), mk(20), mk(30)})

	var got []uint32
	l.Each(func(i int, elem Object) bool {
		if elem.IsNull() {
			t.Fatalf("entry %d unexpectedly null", i)
		}
		// Same Object ObjectAt(i) would return.
		if elem.Uint32(0) != l.ObjectAt(i).Uint32(0) {
			t.Fatalf("entry %d: Each=%d ObjectAt=%d", i, elem.Uint32(0), l.ObjectAt(i).Uint32(0))
		}
		got = append(got, elem.Uint32(0))
		return true
	})
	if len(got) != 3 || got[0] != 10 || got[1] != 20 || got[2] != 30 {
		t.Fatalf("Each yielded %v, want [10 20 30]", got)
	}
}

// TestEachBytes_OverclaimStopsAtRealStream proves a forged length word
// that over-claims the element count cannot fabricate entries: the walk
// stops at the first entry that does not fit the buffer, yielding only
// the real ones — exactly the bound BytesAt enforces per index.
func TestEachBytes_OverclaimStopsAtRealStream(t *testing.T) {
	// One real entry, then forge Len() to a huge value.
	l := buildVarBytesList(t, [][]byte{[]byte("only")})
	l.length = 0x7FFFFFFF // forge the declared count

	var got [][]byte
	l.EachBytes(func(i int, b []byte) bool {
		got = append(got, append([]byte(nil), b...))
		return true
	})
	if len(got) != 1 || string(got[0]) != "only" {
		t.Fatalf("over-claim fabricated entries: got %q, want exactly [only]", got)
	}
}

// TestEachBytes_TruncatedHeader proves a list whose stream is cut mid-way
// (a length prefix promising more bytes than remain) stops cleanly with
// no panic and no over-read.
func TestEachBytes_TruncatedHeader(t *testing.T) {
	l := buildVarBytesList(t, [][]byte{[]byte("ok")})
	// Overwrite the single entry's length prefix to claim more bytes than
	// the buffer holds. l.offset points at that prefix.
	data := l.msg.data
	binary.LittleEndian.PutUint32(data[l.offset:], 0xFFFFFFFF)
	l.length = 1

	called := false
	l.EachBytes(func(i int, b []byte) bool { called = true; return true })
	if called {
		t.Fatal("EachBytes yielded an entry whose payload runs past the buffer")
	}
}

// TestEachBytes_Null proves a null list iterates zero times without panic.
func TestEachBytes_Null(t *testing.T) {
	var l List // zero value: msg == nil
	count := 0
	l.EachBytes(func(i int, b []byte) bool { count++; return true })
	if count != 0 {
		t.Fatalf("null list yielded %d entries, want 0", count)
	}
}

// decodeAllTime returns the median wall time to read every element of an
// n-entry variable-bytes list with the given reader. Empty payloads are
// the minimum-cost real element, maximizing entry count per byte.
func decodeAllTime(t testing.TB, n int, read func(l List) int) time.Duration {
	t.Helper()
	vals := make([][]byte, n) // all empty payloads
	l := buildVarBytesList(t, vals)

	best := time.Duration(1<<63 - 1)
	for rep := 0; rep < 3; rep++ {
		t0 := time.Now()
		got := read(l)
		el := time.Since(t0)
		if got != n {
			t.Fatalf("read %d entries, want %d", got, n)
		}
		if el < best {
			best = el
		}
	}
	return best
}

// readEach counts entries via the single-pass cursor.
func readEach(l List) int {
	c := 0
	l.EachBytes(func(i int, b []byte) bool { c++; return true })
	return c
}

// indexedAllTime times reading every element of an n-entry list via
// per-index BytesAt — the O(N²) path. Used only at small N to log the
// contrast; running it at the cursor's N would take minutes (that is the
// DoS being fixed).
func indexedAllTime(t testing.TB, n int) time.Duration {
	t.Helper()
	l := buildVarBytesList(t, make([][]byte, n))
	best := time.Duration(1<<63 - 1)
	for rep := 0; rep < 3; rep++ {
		t0 := time.Now()
		c := 0
		for i := 0; i < l.Len(); i++ {
			if l.BytesAt(i) == nil {
				break
			}
			c++
		}
		if el := time.Since(t0); el < best {
			best = el
		}
		if c != n {
			t.Fatalf("indexed read %d entries, want %d", c, n)
		}
	}
	return best
}

// TestEachBytes_Linear is the runtime-level regression guard for the
// algorithmic-complexity fix: doubling the element count must roughly
// double EachBytes time (linear). The assertion is on the SCALING SHAPE,
// so it is machine-speed independent.
//
// EachBytes is O(N)-cheap enough that 16k/32k elements decode in tens of
// microseconds — too small to time against scheduler noise. We therefore
// scale to millions of entries, where a linear pass is still only
// milliseconds, and assert the doubling ratio there. The quadratic
// BytesAt walk is timed only at a SMALL N (running it at millions would
// take minutes — exactly the DoS this fix removes) purely to log the
// contrast.
func TestEachBytes_Linear(t *testing.T) {
	if testing.Short() {
		// Wall-clock growth-ratio assertion: reliable on a quiet box, but GC
		// pressure and CPU contention on shared CI runners inflate the ratio
		// (false positives). CI runs `go test -short`; run this locally to
		// guard the single-pass cursor against an O(n^2) re-walk regression.
		t.Skip("perf-sensitive timing assertion; not a deterministic CI gate")
	}
	_ = decodeAllTime(t, 100000, readEach) // warm up allocator/code paths

	const nLow, nHigh = 2_000_000, 4_000_000 // 2x apart, single pass = ms

	eachLow := decodeAllTime(t, nLow, readEach)
	eachHigh := decodeAllTime(t, nHigh, readEach)

	if eachLow < 200*time.Microsecond {
		t.Skipf("low sample too small to time reliably (%s); rerun on a quieter box", eachLow)
	}

	eachRatio := float64(eachHigh) / float64(eachLow)
	t.Logf("EachBytes: n=%d -> %s ; n=%d -> %s ; growth-on-2x = %.2fx (linear ~2x)",
		nLow, eachLow, nHigh, eachHigh, eachRatio)

	// Contrast: the indexed BytesAt walk at a small N (quadratic). At
	// nLow it would be ~(nLow/idxN)^2 times slower — unrunnable — so we
	// sample it only here to show the per-call re-walk really is O(i).
	const idxLow, idxHigh = 8000, 16000
	il := indexedAllTime(t, idxLow)
	ih := indexedAllTime(t, idxHigh)
	if il >= 200*time.Microsecond {
		t.Logf("BytesAt (indexed, small N): n=%d -> %s ; n=%d -> %s ; growth-on-2x = %.2fx (quadratic ~4x)",
			idxLow, il, idxHigh, ih, float64(ih)/float64(il))
	}

	const maxLinearGrowth = 3.0
	if eachRatio >= maxLinearGrowth {
		t.Fatalf("EachBytes is not linear: doubling N (%d->%d) multiplied decode time "+
			"by %.2fx (want < %.1fx). The single-pass cursor must advance p once per "+
			"element, not re-walk the stream from offset 0.",
			nLow, nHigh, eachRatio, maxLinearGrowth)
	}
}
