// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// probe is the Go half of the two-backend proof: it exercises the code the Go
// backend emitted, and prints what it built and what it read. probe.cpp does
// the same over the code the C++ backend emitted from the SAME schemas. The
// proof is that the two outputs are the same file.
//
//	probe write               one line per build case: name and the hex bytes
//	probe back  CASES         read those bytes back: one line of fields per case
//	probe read  VECTORS.TSV   one line per corpus vector: the fields read
//
// `back` is the crossing point: run the Go probe's write output through the
// C++ probe's back, and the C++ reader is reading bytes the Go builder wrote.
//
// Nothing here decides anything. Every value comes from a generated accessor
// or a generated builder, so a disagreement between the two probes is a
// disagreement between the two backends and nowhere else.
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/zap-proto/go/conformance/gen/echo"
	"github.com/zap-proto/go/conformance/gen/pchain"
	"github.com/zap-proto/go/conformance/gen/probe"
	"github.com/zap-proto/go/conformance/gen/xvm"
	"github.com/zap-proto/go/rpc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe write | probe read VECTORS.TSV")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "write":
		write()
	case "back":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "probe back: need a write-output path")
			os.Exit(2)
		}
		back(os.Args[2])
	case "read":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "probe read: need a vectors.tsv path")
			os.Exit(2)
		}
		read(os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "probe: unknown mode %q\n", os.Args[1])
		os.Exit(2)
	}
}

// --- write: every builder, from fixed inputs -------------------------------

func emit(name string, b []byte) {
	fmt.Printf("W\t%s\t%s\n", name, hex.EncodeToString(b))
}

func write() {
	emit("inner/zero", probe.NewInner(probe.InnerInput{}))
	emit("inner/set", probe.NewInner(probe.InnerInput{Tag: 0xA1B2C3D4, Val: 0x0102030405060708}))

	emit("wide/zero", probe.NewWide(probe.WideInput{}))
	emit("wide/full", probe.NewWide(wideFull()))
	emit("wide/lists", probe.NewWide(probe.WideInput{
		Items: [][]byte{{}, {0x01}, longRun(70)},
	}))
	emit("wide/text", probe.NewWide(probe.WideInput{Name: "zero-copy application protocol"}))

	emit("spend/zero", pchain.NewSpend(pchain.SpendInput{}))
	emit("spend/full", pchain.NewSpend(pchain.SpendInput{
		Kind:         3,
		NetworkID:    1,
		BlockchainID: id32(0x09),
		Outs:         [][]byte{longRun(72), longRun(72)},
		OwnerAddrs:   [][]byte{longRun(20)},
		Ins:          [][]byte{longRun(96)},
		SigIndices:   [][]byte{{0, 0, 0, 0}},
		Memo:         []byte("conformance memo"),
	}))

	emit("basetx/zero", xvm.NewBaseTx(xvm.BaseTxInput{}))
	emit("basetx/full", xvm.NewBaseTx(xvm.BaseTxInput{
		NetworkID:    0xFFFFFFFF,
		BlockchainID: id32(0xAB),
		Outs:         [][]byte{probe.NewInner(probe.InnerInput{Tag: 1, Val: 2})},
		Ins:          [][]byte{probe.NewInner(probe.InnerInput{Tag: 3, Val: 4})},
		Memo:         []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}))

	writeEcho()
}

// wideFull sets every field of Wide, so every emitted writer runs.
func wideFull() probe.WideInput {
	var id [16]byte
	for i := range id {
		id[i] = byte(i)
	}
	return probe.WideInput{
		Flag:  true,
		A8:    0xAB,
		A16:   0xBEEF,
		A32:   0xDEADBEEF,
		A64:   0x0123456789ABCDEF,
		S8:    -3,
		S16:   -300,
		S32:   -70000,
		S64:   -5000000000000,
		F32:   -1.5,
		F64:   3.141592653589793,
		Id:    id,
		Name:  "zap",
		Blob:  []byte{0x00, 0x01, 0x02, 0x03, 0x04},
		Items: [][]byte{{0xAA}, {0xBB, 0xCC}, {}},
		Child: probe.NewInner(probe.InnerInput{Tag: 7, Val: 9}),
	}
}

func id32(fill byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = fill
	}
	return out
}

func longRun(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i)
	}
	return out
}

// --- write: the service envelopes -----------------------------------------

// recorder is an Echo channel that keeps the request envelope the generated
// client shipped and answers with a fixed response.
type recorder struct{ env []byte }

func (r *recorder) Call(envelope []byte) (rpc.Response, error) {
	r.env = envelope
	return rpc.Response{Status: rpc.StatusOK, PromiseID: 1, Body: []byte{0x10, 0x20}}, nil
}

// handler answers every Echo method with a fixed body, so Dispatch's response
// envelopes are comparable across the two languages.
type handler struct{}

func (handler) Ping(req []byte) ([]byte, error) { return []byte{0x01, 0x02, 0x03}, nil }
func (handler) Notify(req []byte) error         { return nil }
func (handler) Health() ([]byte, error)         { return []byte{0x04}, nil }
func (handler) Shutdown() error                 { return nil }

func writeEcho() {
	cap := []byte{0xCA, 0xFE}
	payload := []byte{0x11, 0x22, 0x33}

	// One fresh client per case, so each request carries promise id 1 and the
	// bytes do not depend on how many calls ran before.
	r := &recorder{}
	echo.NewEchoClient(r, cap).Ping(payload)
	emit("echo/req/ping", r.env)

	r = &recorder{}
	echo.NewEchoClient(r, cap).Notify(payload)
	emit("echo/req/notify", r.env)

	r = &recorder{}
	echo.NewEchoClient(r, cap).Health()
	emit("echo/req/health", r.env)

	r = &recorder{}
	echo.NewEchoClient(r, cap).Shutdown()
	emit("echo/req/shutdown", r.env)

	// The pipelined form: a dependent call targeting the first call's promise.
	r = &recorder{}
	c := echo.NewEchoClient(r, cap)
	p, _, _ := c.Ping(payload)
	c.PingOn(p)
	emit("echo/req/ping-on", r.env)

	// And the server half: the response envelope Dispatch builds for each
	// ordinal, plus the refusal an unknown ordinal earns.
	var h handler
	for _, name := range []string{"ping", "notify", "health", "shutdown"} {
		req := requestFor(name, cap, payload)
		resp, err := echo.DispatchEcho(h, req)
		if err != nil {
			resp = nil
		}
		emit("echo/resp/"+name, resp)
	}
	unknown := rpc.BuildRequest(rpc.Call{Method: 99, PromiseID: 1, Target: rpc.NoTarget, Cap: cap})
	resp, _ := echo.DispatchEcho(h, unknown)
	emit("echo/resp/unknown", resp)
}

// requestFor rebuilds one method's request envelope through the generated
// client, so Dispatch is fed exactly what the client ships.
func requestFor(method string, cap, payload []byte) []byte {
	r := &recorder{}
	c := echo.NewEchoClient(r, cap)
	switch method {
	case "ping":
		c.Ping(payload)
	case "notify":
		c.Notify(payload)
	case "health":
		c.Health()
	case "shutdown":
		c.Shutdown()
	}
	return r.env
}

// --- read: the generated reader over the corpus ----------------------------

// read walks a corpus file and prints, for every P-chain transaction vector,
// what the generated Spend reader says about the bytes. Malformed and
// truncated vectors are read too — a total reader answers zero rather than
// faulting, and the two languages have to answer zero in the same places.
func read(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe read: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "V\t") {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) < 5 || col[2] != "P" || col[3] != "tx" {
			continue
		}
		fmt.Printf("R\t%s\t%s\n", col[1], spendLine(col[4]))
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "probe read: %v\n", err)
		os.Exit(1)
	}
}

func spendLine(wire string) string {
	raw, err := hex.DecodeString(wire)
	if err != nil {
		return "wire=unhex"
	}
	return spendFields(raw)
}

func spendFields(raw []byte) string {
	s, err := pchain.WrapSpend(raw)
	if err != nil {
		return "parse=err"
	}
	return fmt.Sprintf("parse=ok kind=%d net=%d chain=%s outs=%d owners=%d ins=%d sigs=%d memo=%s",
		s.Kind(), s.NetworkID(), hexOf(s.BlockchainID()),
		s.Outs().Len(), s.OwnerAddrs().Len(), s.Ins().Len(), s.SigIndices().Len(),
		hex.EncodeToString(s.Memo()))
}

func hexOf(a [32]byte) string { return hex.EncodeToString(a[:]) }

// --- back: every reader, over bytes the other language may have written -----

// back reads a write-mode output file and prints what the generated readers
// say about each case's bytes. Fed the C++ probe's output it reads C++ bytes;
// fed its own it reads its own. Either way the fields printed come only from
// generated accessors.
func back(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe back: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		col := strings.Split(sc.Text(), "\t")
		if len(col) < 3 || col[0] != "W" {
			continue
		}
		raw, err := hex.DecodeString(col[2])
		if err != nil {
			fmt.Printf("B\t%s\twire=unhex\n", col[1])
			continue
		}
		fmt.Printf("B\t%s\t%s\n", col[1], backLine(col[1], raw))
	}
}

func backLine(name string, raw []byte) string {
	switch {
	case strings.HasPrefix(name, "inner/"):
		v, err := probe.WrapInner(raw)
		if err != nil {
			return "parse=err"
		}
		return fmt.Sprintf("tag=%d val=%d", v.Tag(), v.Val())
	case strings.HasPrefix(name, "wide/"):
		return wideLine(raw)
	case strings.HasPrefix(name, "spend/"):
		return spendFields(raw)
	case strings.HasPrefix(name, "basetx/"):
		v, err := xvm.WrapBaseTx(raw)
		if err != nil {
			return "parse=err"
		}
		id := v.BlockchainID()
		return fmt.Sprintf("net=%d chain=%s outs=%d ins=%d memo=%s",
			v.NetworkID(), hex.EncodeToString(id[:]), v.Outs().Len(), v.Ins().Len(),
			hex.EncodeToString(v.Memo()))
	case strings.HasPrefix(name, "echo/req/"):
		c, err := rpc.ParseRequest(raw)
		if err != nil {
			return "parse=err"
		}
		return fmt.Sprintf("method=%d promise=%d target=%d cap=%s payload=%s",
			c.Method, c.PromiseID, c.Target, hex.EncodeToString(c.Cap), hex.EncodeToString(c.Payload))
	case strings.HasPrefix(name, "echo/resp/"):
		r, err := rpc.ParseResponse(raw)
		if err != nil {
			return "parse=err"
		}
		return fmt.Sprintf("status=%d promise=%d body=%s",
			r.Status, r.PromiseID, hex.EncodeToString(r.Body))
	}
	return "case=unknown"
}

// wideLine reads every field of Wide. Floats are printed as their bits: the
// accessor still runs, and two languages cannot disagree about how to format
// a float when neither formats one.
func wideLine(raw []byte) string {
	v, err := probe.WrapWide(raw)
	if err != nil {
		return "parse=err"
	}
	id := v.Id()
	child := v.Child()
	return fmt.Sprintf("flag=%t a8=%d a16=%d a32=%d a64=%d s8=%d s16=%d s32=%d s64=%d "+
		"f32=%08x f64=%016x id=%s name=%s blob=%s items=%d child=%d:%d",
		v.Flag(), v.A8(), v.A16(), v.A32(), v.A64(), v.S8(), v.S16(), v.S32(), v.S64(),
		math.Float32bits(v.F32()), math.Float64bits(v.F64()),
		hex.EncodeToString(id[:]), v.Name(), hex.EncodeToString(v.Blob()),
		v.Items().Len(), child.Tag(), child.Val())
}
