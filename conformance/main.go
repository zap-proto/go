// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The cross-language proof. This program and its Rust twin (conformance/rust)
// read the same corpus, run the same schemas through the code zapgen emitted
// for each language, and write the same report. The report is the proof: if
// the two files are not byte-identical, the two backends disagree about the
// wire and the diff says where.
//
// What it covers:
//
//   - build — a fixed input through the emitted builder, and the bytes it
//     wrote. Every type in the dialect appears in the kitchen schema.
//
//   - read — the digest of every field the emitted reader answers, over the
//     bytes just built and over the corpus.
//
//   - corpus — 208 real P-chain and X-chain vectors, written by the Go node.
//     Each one is parsed, read, written back out, and read again.
//
//   - envelope — the call framing a generated client and dispatch ride on.
//
//     go run ./conformance > /tmp/go.tsv
//     cargo run --manifest-path conformance/rust/Cargo.toml > /tmp/rust.tsv
//     cmp /tmp/go.tsv /tmp/rust.tsv
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	zap "github.com/zap-proto/go"
	"github.com/zap-proto/go/conformance/echo"
	"github.com/zap-proto/go/conformance/kitchen"
	"github.com/zap-proto/go/conformance/pchain"
	"github.com/zap-proto/go/conformance/xchain"
	"github.com/zap-proto/go/rpc"
)

func main() {
	corpus := flag.String("corpus", "conformance/corpus/vectors.tsv", "the chain vector corpus")
	flag.Parse()

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	built(w)
	envelope(w)
	client(w)
	if err := corpusRun(w, *corpus); err != nil {
		fmt.Fprintln(os.Stderr, "zapgen proof:", err)
		os.Exit(1)
	}
}

// --- built: a fixed input through the emitted builder -----------------------

func built(w *bufio.Writer) {
	leaf := kitchen.NewLeaf(kitchen.LeafInput{Tag: 7, Note: "leaf note"})
	line(w, "BUILD", "kitchen.Leaf", hex.EncodeToString(leaf))
	line(w, "READ", "kitchen.Leaf", kitchen.LeafOf(leaf))

	all := kitchen.NewAll(kitchen.AllInput{
		Flag: true,
		A8:   0x7f,
		A16:  0xbeef,
		A32:  0xdeadbeef,
		A64:  0x0123456789abcdef,
		S8:   -8,
		S16:  -300,
		S32:  -70000,
		S64:  -5000000000,
		F32:  0.15625,
		F64:  -2.718281828459045,
		Name: "a name with ünïcödé",
		Blob: []byte{0x00, 0x01, 0xfe, 0xff},
		Id:   [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Items: [][]byte{
			kitchen.NewLeaf(kitchen.LeafInput{Tag: 1, Note: "one"}),
			kitchen.NewLeaf(kitchen.LeafInput{Tag: 2, Note: ""}),
			kitchen.NewLeaf(kitchen.LeafInput{Tag: 0xffffffff, Note: "three"}),
		},
		Inner: leaf,
	})
	line(w, "BUILD", "kitchen.All", hex.EncodeToString(all))
	line(w, "READ", "kitchen.All", kitchen.AllOf(all))

	// The empty case: every field left at its zero.
	zero := kitchen.NewAll(kitchen.AllInput{})
	line(w, "BUILD", "kitchen.All.zero", hex.EncodeToString(zero))
	line(w, "READ", "kitchen.All.zero", kitchen.AllOf(zero))

	ping := echo.NewPing(echo.PingInput{Seq: 0xfeedface})
	line(w, "BUILD", "echo.Ping", hex.EncodeToString(ping))
}

// --- envelope: the framing a generated client and dispatch ride on ----------

func envelope(w *bufio.Writer) {
	req := rpc.BuildRequest(rpc.Call{
		Method:    echo.EchoPingOrdinal,
		PromiseID: 1,
		Target:    rpc.NoTarget,
		Cap:       []byte{0xca, 0xfe},
		Payload:   echo.NewPing(echo.PingInput{Seq: 42}),
	})
	line(w, "BUILD", "rpc.request", hex.EncodeToString(req))

	call, err := rpc.ParseRequest(req)
	if err != nil {
		line(w, "READ", "rpc.request", "err="+err.Error())
	} else {
		line(w, "READ", "rpc.request", fmt.Sprintf("method=%d;promise=%d;target=%d;cap=%x;payload=%x",
			call.Method, call.PromiseID, call.Target, call.Cap, call.Payload))
	}

	resp := rpc.BuildResponse(rpc.StatusOK, 1, echo.NewPong(echo.PongInput{Seq: 43}))
	line(w, "BUILD", "rpc.response", hex.EncodeToString(resp))

	// The generated dispatch, over the generated handler contract.
	out, err := echo.DispatchEcho(handler{}, req)
	if err != nil {
		line(w, "BUILD", "echo.dispatch.ping", "err="+err.Error())
	} else {
		line(w, "BUILD", "echo.dispatch.ping", hex.EncodeToString(out))
	}

	unknown := rpc.BuildRequest(rpc.Call{Method: 99, PromiseID: 5})
	out, err = echo.DispatchEcho(handler{}, unknown)
	if err != nil {
		line(w, "BUILD", "echo.dispatch.unknown", "err="+err.Error())
	} else {
		line(w, "BUILD", "echo.dispatch.unknown", hex.EncodeToString(out))
	}

	out, err = echo.DispatchEcho(faulty{}, req)
	if err != nil {
		line(w, "BUILD", "echo.dispatch.fault", "err="+err.Error())
	} else {
		line(w, "BUILD", "echo.dispatch.fault", hex.EncodeToString(out))
	}
}

// --- client: the emitted client, over a channel that dispatches in place --

// loop is a channel that carries a call straight into the dispatch, so the
// emitted client and the emitted server meet with no transport in between.
type loop struct {
	w *bufio.Writer
	n int
}

func (l *loop) Call(envelope []byte) (rpc.Response, error) {
	line(l.w, "CALL", fmt.Sprintf("%d", l.n), hex.EncodeToString(envelope))
	l.n++
	out, err := echo.DispatchEcho(handler{}, envelope)
	if err != nil {
		return rpc.Response{}, err
	}
	return rpc.ParseResponse(out)
}

func client(w *bufio.Writer) {
	c := echo.NewEchoClient(&loop{w: w}, []byte{0xca, 0xfe})

	p, body, err := c.Ping(echo.NewPing(echo.PingInput{Seq: 100}))
	line(w, "CLIENT", "ping", answer(p, body, err))

	// The pipelined form: its payload is the earlier call's answer, supplied
	// server-side, so it ships with no payload of its own.
	p2, body2, err := c.PingOn(p)
	line(w, "CLIENT", "ping_on", answer(p2, body2, err))

	p3, err := c.Notify(echo.NewPing(echo.PingInput{Seq: 1}))
	line(w, "CLIENT", "notify", answer(p3, nil, err))

	p4, body4, err := c.Health()
	line(w, "CLIENT", "health", answer(p4, body4, err))
}

// answer renders one call's outcome the same way in both languages.
//
// The refusal is rendered as the bare word, not as the error's text: the Go
// client answers a formatted error and the Rust client a typed one, and the
// wording of a refusal is idiom, not wire. That a call was refused, and the
// bytes of every envelope that carried it, are compared.
func answer(p rpc.Promise, body []byte, err error) string {
	if err != nil {
		return fmt.Sprintf("promise=%d;refused", p.ID)
	}
	return fmt.Sprintf("promise=%d;body=%x", p.ID, body)
}

// handler answers a ping with a pong of the next sequence number.
type handler struct{}

func (handler) Ping(req []byte) ([]byte, error) {
	p, err := echo.WrapPing(req)
	if err != nil {
		return nil, err
	}
	return echo.NewPong(echo.PongInput{Seq: p.Seq() + 1}), nil
}
func (handler) Notify(req []byte) error { return nil }
func (handler) Health() ([]byte, error) { return echo.NewPong(echo.PongInput{Seq: 0}), nil }
func (handler) Shutdown() error         { return nil }

// faulty refuses every call, which is how the internal status gets exercised.
type faulty struct{}

func (faulty) Ping(req []byte) ([]byte, error) { return nil, fmt.Errorf("no") }
func (faulty) Notify(req []byte) error         { return fmt.Errorf("no") }
func (faulty) Health() ([]byte, error)         { return nil, fmt.Errorf("no") }
func (faulty) Shutdown() error                 { return fmt.Errorf("no") }

// --- corpus -----------------------------------------------------------------

func corpusRun(w *bufio.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		text := sc.Text()
		if strings.HasPrefix(text, "#") || strings.TrimSpace(text) == "" {
			continue
		}
		col := strings.Split(text, "\t")
		if len(col) < 5 || col[0] != "V" {
			continue
		}
		id, chain, op, wire := col[1], col[2], col[3], col[4]
		if wire == "-" || wire == "" {
			line(w, "V", id, "nowire")
			continue
		}
		b, err := hex.DecodeString(wire)
		if err != nil {
			line(w, "V", id, "badhex")
			continue
		}
		vector(w, id, chain, op, b)
	}
	return sc.Err()
}

func vector(w *bufio.Writer, id, chain, op string, b []byte) {
	// What the runtime says about the bytes, before any schema is applied.
	head := chain + "/" + op
	if m, err := zap.Parse(zapBody(chain, op, b)); err != nil {
		line(w, "V", id, head+";parse=err:"+err.Error())
		return
	} else {
		line(w, "V", id, fmt.Sprintf("%s;parse=ok;version=%d;flags=%d;size=%d",
			head, m.Version(), m.Flags(), m.Size()))
	}

	switch {
	case chain == "P" && op == "tx":
		d, err := pchain.SpendOf(b)
		lineErr(w, "R", id, d, err)
		out, err := pchain.Rebuild(b)
		if err != nil {
			line(w, "W", id, "err="+err.Error())
			return
		}
		line(w, "W", id, hex.EncodeToString(out))
		line(w, "EQ", id, same(out, b))
		d, err = pchain.SpendOf(out)
		lineErr(w, "RR", id, d, err)
	case chain == "P" && op == "block":
		d, err := pchain.BlockOf(b)
		lineErr(w, "R", id, d, err)
	case chain == "X" && op == "tx":
		d, err := xchain.SignedOf(b)
		lineErr(w, "R", id, d, err)
		out, err := xchain.RebuildSigned(b)
		if err != nil {
			line(w, "W", id, "err="+err.Error())
			return
		}
		line(w, "W", id, hex.EncodeToString(out))
		line(w, "EQ", id, same(out, b[xchain.Prefix:]))
		d, err = xchain.SignedOf(prefixed(out))
		lineErr(w, "RR", id, d, err)
	case chain == "X" && op == "block":
		d, err := xchain.BlockOf(b)
		lineErr(w, "R", id, d, err)
	}
}

// zapBody strips the chain's own framing. An X TRANSACTION carries a type
// byte and a shape byte ahead of the ZAP message; an X block and every P
// vector carry none.
func zapBody(chain, op string, b []byte) []byte {
	if chain == "X" && op == "tx" && len(b) >= xchain.Prefix {
		return b[xchain.Prefix:]
	}
	return b
}

// prefixed puts the two X framing bytes back so a rebuilt envelope can be
// read by the same reader that read the original.
func prefixed(b []byte) []byte {
	return append([]byte{0, 0}, b...)
}

// same says whether what the generated builder wrote is what the chain
// wrote. A chain vector may carry more than one message — a P transaction is
// its unsigned bytes with a credential message concatenated — so the
// comparison is against the FIRST message, whose length its own header
// declares. "no" carries where the two part, because a byte offset is the
// only useful thing to say about a disagreement of bytes.
func same(built, wire []byte) string {
	n := declared(wire)
	if n == 0 || n > len(wire) {
		return "no;the vector declares no message"
	}
	head := wire[:n]
	if len(built) != len(head) {
		return fmt.Sprintf("no;size=%d;chain=%d", len(built), len(head))
	}
	for i := range built {
		if built[i] != head[i] {
			return fmt.Sprintf("no;at=%d", i)
		}
	}
	return "yes"
}

// declared is the message size the ZAP header states, or 0 for bytes that do
// not open one.
func declared(b []byte) int {
	if len(b) < zap.HeaderSize {
		return 0
	}
	return int(binary.LittleEndian.Uint32(b[12:16]))
}

func line(w *bufio.Writer, kind, id, body string) {
	fmt.Fprintf(w, "%s\t%s\t%s\n", kind, id, body)
}

func lineErr(w *bufio.Writer, kind, id, body string, err error) {
	if err != nil {
		line(w, kind, id, "err="+err.Error())
		return
	}
	line(w, kind, id, body)
}
