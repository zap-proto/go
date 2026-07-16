// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

// buildDBRequest builds a request message the exact way a ZAP-native DB
// client does: flags = msg_type (300/301/...), a root object carrying the
// request path as Text@4 and the JSON body as Bytes@12. Mirrors
// hanzoai/orm's db/zap.go request construction so this test also guards
// that shape.
func buildDBRequest(msgType uint16, path string, body []byte) *Message {
	b := NewBuilder(len(path) + len(body) + 128)
	ob := b.StartObject(20)
	ob.SetText(4, path)
	ob.SetBytes(12, body)
	ob.FinishAsRoot()
	data := b.FinishWithFlags(msgType)
	m, err := Parse(data)
	if err != nil {
		panic(err)
	}
	return m
}

// mockDBServer speaks the one-message-per-connection ZAP DB wire protocol:
// accept, read one framed request, hand (msgType, path, body) to a handler,
// write the one-message response, close. This is the in-process stand-in
// for hanzo/sql's zap_fdw listener — no live :9651 needed.
type mockDBServer struct {
	ln      net.Listener
	handler func(msgType uint16, path string, body []byte) (status uint32, respBody []byte)
	delay   time.Duration // optional per-request delay, for cancellation tests
}

func startMockDBServer(t *testing.T, delay time.Duration, handler func(uint16, string, []byte) (uint32, []byte)) *mockDBServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &mockDBServer{ln: ln, handler: handler, delay: delay}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *mockDBServer) addr() string { return s.ln.Addr().String() }

func (s *mockDBServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *mockDBServer) handleConn(conn net.Conn) {
	defer conn.Close()
	msg, err := readFramedMessage(conn)
	if err != nil {
		return
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	root := msg.Root()
	path := root.Text(4)
	body := root.Bytes(12)
	status, respBody := s.handler(msg.Flags(), path, append([]byte(nil), body...))

	// Response: status@0 (u32), body@4 (bytes). Same layout the C backend's
	// zap_build_response emits and the driver's response reader expects.
	b := NewBuilder(len(respBody) + 64)
	ob := b.StartObject(20)
	ob.SetUint32(0, status)
	ob.SetBytes(4, respBody)
	ob.FinishAsRoot()
	_, _ = conn.Write(b.FinishWithFlags(0))
}

func TestNode_CallRoundTrip(t *testing.T) {
	const msgTypeSQL uint16 = 300
	srv := startMockDBServer(t, 0, func(mt uint16, path string, body []byte) (uint32, []byte) {
		if mt != msgTypeSQL {
			t.Errorf("server saw msgType %d, want %d", mt, msgTypeSQL)
		}
		if path != "/query" {
			t.Errorf("server saw path %q, want /query", path)
		}
		if string(body) != `{"sql":"SELECT 1"}` {
			t.Errorf("server saw body %q", body)
		}
		return 200, []byte(`[{"data":{"ok":true}}]`)
	})

	n := NewNode(NodeConfig{NodeID: "test-client", NoDiscovery: true})
	if err := n.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop()
	if err := n.ConnectDirect(srv.addr()); err != nil {
		t.Fatalf("ConnectDirect: %v", err)
	}

	peers := n.Peers()
	if len(peers) != 1 || peers[0] != srv.addr() {
		t.Fatalf("Peers() = %v, want [%s]", peers, srv.addr())
	}

	req := buildDBRequest(msgTypeSQL, "/query", []byte(`{"sql":"SELECT 1"}`))
	resp, err := n.Call(context.Background(), peers[0], req)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	root := resp.Root()
	if got := root.Uint32(0); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := string(root.Bytes(4)); got != `[{"data":{"ok":true}}]` {
		t.Fatalf("body = %q", got)
	}
}

func TestNode_UnknownPeer(t *testing.T) {
	n := NewNode(NodeConfig{NoDiscovery: true})
	_ = n.Start()
	req := buildDBRequest(300, "/query", []byte("{}"))
	_, err := n.Call(context.Background(), "127.0.0.1:1", req)
	if !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("Call to unregistered peer = %v, want ErrUnknownPeer", err)
	}
}

func TestNode_ConnectDirectUnreachable(t *testing.T) {
	n := NewNode(NodeConfig{NoDiscovery: true, DialTimeout: 500 * time.Millisecond})
	_ = n.Start()
	// Reserved TEST-NET-1 address that does not accept connections.
	err := n.ConnectDirect("192.0.2.1:9651")
	if err == nil {
		t.Fatal("ConnectDirect to unreachable addr returned nil, want error")
	}
	if len(n.Peers()) != 0 {
		t.Fatalf("Peers() = %v after failed connect, want empty", n.Peers())
	}
}

func TestNode_ContextCancel(t *testing.T) {
	srv := startMockDBServer(t, 2*time.Second, func(uint16, string, []byte) (uint32, []byte) {
		return 200, []byte("late")
	})
	n := NewNode(NodeConfig{NoDiscovery: true})
	_ = n.Start()
	if err := n.ConnectDirect(srv.addr()); err != nil {
		t.Fatalf("ConnectDirect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := buildDBRequest(300, "/query", []byte("{}"))
	start := time.Now()
	_, err := n.Call(ctx, srv.addr(), req)
	if err == nil {
		t.Fatal("Call with expiring ctx returned nil, want deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call err = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Call took %v, should have aborted near the 100ms deadline", elapsed)
	}
}

func TestNode_StopRejectsCalls(t *testing.T) {
	srv := startMockDBServer(t, 0, func(uint16, string, []byte) (uint32, []byte) {
		return 200, []byte("ok")
	})
	n := NewNode(NodeConfig{NoDiscovery: true})
	_ = n.Start()
	if err := n.ConnectDirect(srv.addr()); err != nil {
		t.Fatalf("ConnectDirect: %v", err)
	}
	addr := srv.addr()
	n.Stop()
	req := buildDBRequest(300, "/query", []byte("{}"))
	if _, err := n.Call(context.Background(), addr, req); !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("Call after Stop = %v, want ErrNodeStopped", err)
	}
	if err := n.Start(); !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("Start after Stop = %v, want ErrNodeStopped", err)
	}
}

// TestReadFramedMessage_BackendWireFormat proves the Node reads the EXACT
// byte layout hanzo/sql's C zap_build_response emits (header16 + root20 +
// body), so a live backend's response parses correctly. Hand-built here so
// the guarantee holds with no live :9651.
func TestReadFramedMessage_BackendWireFormat(t *testing.T) {
	body := []byte(`{"rows":1}`)
	// Layout from zap_protocol.h zap_build_response:
	//   total = align8(16 + 20 + len(body))
	total := 16 + 20 + len(body)
	total = (total + 7) &^ 7
	buf := make([]byte, total)
	copy(buf[0:4], Magic)
	binary.LittleEndian.PutUint16(buf[4:6], 1) // version 1
	binary.LittleEndian.PutUint16(buf[6:8], 0) // flags
	binary.LittleEndian.PutUint32(buf[8:12], 16)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(total))
	root := 16
	binary.LittleEndian.PutUint32(buf[root+0:], 200) // status @0
	// body bytes field @4: relative offset then length; data at root+20.
	binary.LittleEndian.PutUint32(buf[root+4:], uint32(20-4)) // rel = 16
	binary.LittleEndian.PutUint32(buf[root+8:], uint32(len(body)))
	copy(buf[root+20:], body)

	msg, err := readFramedMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("readFramedMessage: %v", err)
	}
	r := msg.Root()
	if got := r.Uint32(0); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := string(r.Bytes(4)); got != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

// TestReadFramedMessage_Fragmented proves the header-Size framing tolerates
// TCP segmentation: the response arrives in two writes and still parses.
func TestReadFramedMessage_Fragmented(t *testing.T) {
	body := []byte("hello-fragmented-world")
	b := NewBuilder(len(body) + 64)
	ob := b.StartObject(20)
	ob.SetUint32(0, 200)
	ob.SetBytes(4, body)
	ob.FinishAsRoot()
	full := b.FinishWithFlags(0)

	pr, pw := net.Pipe()
	go func() {
		defer pw.Close()
		split := 20 // mid-message split, past the header
		_, _ = pw.Write(full[:split])
		time.Sleep(20 * time.Millisecond)
		_, _ = pw.Write(full[split:])
	}()
	msg, err := readFramedMessage(pr)
	if err != nil {
		t.Fatalf("readFramedMessage(fragmented): %v", err)
	}
	if got := string(msg.Root().Bytes(4)); got != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
}
