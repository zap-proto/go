// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package echo

import (
	"testing"

	"github.com/zap-proto/go/transport"
)

// Compile-time proof that the network transport's *Conn satisfies the
// codegen-emitted client channel interface verbatim — i.e. a generated
// client takes it with no adapter.
var _ EchoChannel = (*transport.Conn)(nil)

// TestEcho_OverNetworkTransport drives the REAL generated EchoClient against
// the REAL generated DispatchEcho, but over a live socket via the transport
// package instead of the in-memory loopback. This is the drop-in proof: the
// generated code is byte-for-byte unchanged; only the channel is now a
// network connection. No protobuf, no gRPC — ZAP envelopes over a socket.
func TestEcho_OverNetworkTransport(t *testing.T) {
	h := &echoHandler{}

	srv, err := transport.Listen("tcp", "127.0.0.1:0", func(env []byte) ([]byte, error) {
		return DispatchEcho(h, env)
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	conn, err := transport.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	client := NewEchoClient(conn, nil)

	// Unary request/response over the wire: Ping(Seq=41) -> Pong(Seq=42).
	_, body, err := client.Ping(NewPing(PingInput{Seq: 41}))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	pong, err := WrapPong(body)
	if err != nil {
		t.Fatalf("WrapPong: %v", err)
	}
	if pong.Seq() != 42 {
		t.Fatalf("Pong.Seq = %d, want 42", pong.Seq())
	}

	// A void-return method over the wire.
	if _, err := client.Notify(NewPing(PingInput{Seq: 7})); err != nil {
		t.Fatalf("Notify: %v", err)
	}
}
