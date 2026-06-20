// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package echo

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zap-proto/go/rpc"
)

// echoHandler is a hand-written EchoHandler that implements the generated
// server contract. ping echoes Seq+1; notify records the last seq; health
// returns a fixed Pong; shutdown flips a flag. failPing makes ping error
// so the dispatcher's StatusInternal path is exercised.
type echoHandler struct {
	lastNotify uint64
	stopped    bool
	failPing   bool
}

func (h *echoHandler) Ping(req []byte) ([]byte, error) {
	if h.failPing {
		return nil, errors.New("boom")
	}
	p, err := WrapPing(req)
	if err != nil {
		return nil, err
	}
	return NewPong(PongInput{Seq: p.Seq() + 1}), nil
}

func (h *echoHandler) Notify(req []byte) error {
	p, err := WrapPing(req)
	if err != nil {
		return err
	}
	h.lastNotify = p.Seq()
	return nil
}

func (h *echoHandler) Health() ([]byte, error) {
	return NewPong(PongInput{Seq: 1}), nil
}

func (h *echoHandler) Shutdown() error {
	h.stopped = true
	return nil
}

// loopback is an EchoChannel that routes each request envelope straight
// through DispatchEcho against h, then parses the response — the whole RPC
// loop without a socket.
type loopback struct{ h *echoHandler }

func (l loopback) Call(envelope []byte) (rpc.Response, error) {
	respBytes, err := DispatchEcho(l.h, envelope)
	if err != nil {
		return rpc.Response{}, err
	}
	return rpc.ParseResponse(respBytes)
}

func newClient(h *echoHandler) *EchoClient {
	return NewEchoClient(loopback{h: h}, nil)
}

// TestRequestResponse drives the request+response method end-to-end.
func TestRequestResponse(t *testing.T) {
	c := newClient(&echoHandler{})
	body, err := c.Ping(NewPing(PingInput{Seq: 41}))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	pong, err := WrapPong(body)
	if err != nil {
		t.Fatalf("WrapPong: %v", err)
	}
	if pong.Seq() != 42 {
		t.Errorf("Pong.Seq = %d, want 42", pong.Seq())
	}
}

// TestVoidReturn drives a request-only (no response) method.
func TestVoidReturn(t *testing.T) {
	h := &echoHandler{}
	c := newClient(h)
	if err := c.Notify(NewPing(PingInput{Seq: 7})); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if h.lastNotify != 7 {
		t.Errorf("handler.lastNotify = %d, want 7", h.lastNotify)
	}
}

// TestNoRequest drives a response-only (no request param) method.
func TestNoRequest(t *testing.T) {
	c := newClient(&echoHandler{})
	body, err := c.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if _, err := WrapPong(body); err != nil {
		t.Errorf("Health body is not a Pong: %v", err)
	}
}

// TestBareMethod drives a method with no params and no return.
func TestBareMethod(t *testing.T) {
	h := &echoHandler{}
	c := newClient(h)
	if err := c.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !h.stopped {
		t.Errorf("handler not stopped")
	}
}

// TestHandlerErrorIsStatusInternal proves a handler error surfaces as a
// non-OK status to the client, not a transport error.
func TestHandlerErrorIsStatusInternal(t *testing.T) {
	c := newClient(&echoHandler{failPing: true})
	if _, err := c.Ping(NewPing(PingInput{Seq: 1})); err == nil {
		t.Errorf("expected error from failing Ping")
	}
}

// TestUnknownOrdinalIsNotFound proves DispatchEcho returns StatusNotFound
// for an ordinal no method owns (forward/backward wire compatibility: an
// old server politely rejects a new method instead of mis-routing).
func TestUnknownOrdinalIsNotFound(t *testing.T) {
	env := rpc.BuildRequest(rpc.Call{Method: 99, PromiseID: 5})
	respBytes, err := DispatchEcho(&echoHandler{}, env)
	if err != nil {
		t.Fatalf("DispatchEcho: %v", err)
	}
	resp, err := rpc.ParseResponse(respBytes)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if resp.Status != rpc.StatusNotFound {
		t.Errorf("Status = %d, want StatusNotFound", resp.Status)
	}
	if resp.PromiseID != 5 {
		t.Errorf("PromiseID = %d, want 5 (echoed)", resp.PromiseID)
	}
}

// TestOrdinalsAreOneBased pins the wire ordinals to the 1,2,3,4 the spec
// mandates (declaration order, never renumbered on append).
func TestOrdinalsAreOneBased(t *testing.T) {
	if EchoPingOrdinal != 1 || EchoNotifyOrdinal != 2 || EchoHealthOrdinal != 3 || EchoShutdownOrdinal != 4 {
		t.Errorf("ordinals = %d,%d,%d,%d, want 1,2,3,4",
			EchoPingOrdinal, EchoNotifyOrdinal, EchoHealthOrdinal, EchoShutdownOrdinal)
	}
}

// TestCapForwarded proves the client attaches its capability bytes to
// every request envelope (the dispatcher sees them).
func TestCapForwarded(t *testing.T) {
	want := []byte("my-cap-token")
	var seen []byte
	ch := capSpy{onCall: func(env []byte) {
		call, _ := rpc.ParseRequest(env)
		seen = call.Cap
	}, h: &echoHandler{}}
	c := NewEchoClient(ch, want)
	if _, err := c.Ping(NewPing(PingInput{Seq: 1})); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(seen, want) {
		t.Errorf("forwarded cap = %q, want %q", seen, want)
	}
}

type capSpy struct {
	onCall func([]byte)
	h      *echoHandler
}

func (s capSpy) Call(envelope []byte) (rpc.Response, error) {
	s.onCall(envelope)
	respBytes, err := DispatchEcho(s.h, envelope)
	if err != nil {
		return rpc.Response{}, err
	}
	return rpc.ParseResponse(respBytes)
}
