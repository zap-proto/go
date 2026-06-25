// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// TestServerStream proves server-streaming: the client opens a stream with an
// initial payload, the server pushes N messages and returns (half-close), and
// the client receives all N then io.EOF.
func TestServerStream(t *testing.T) {
	sh := func(method uint32, init []byte, s *Stream) {
		if method != 7 {
			return
		}
		for i := 0; i < 3; i++ {
			if err := s.Send([]byte(fmt.Sprintf("%s-%d", init, i))); err != nil {
				return
			}
		}
		// returning half-closes the send side
	}
	srv, err := ListenStream("tcp", "127.0.0.1:0", nil, sh)
	if err != nil {
		t.Fatalf("ListenStream: %v", err)
	}
	defer srv.Close()

	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	st, err := conn.OpenStream(7, []byte("hi"))
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	var got []string
	for {
		msg, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, string(msg))
	}
	if len(got) != 3 || got[0] != "hi-0" || got[1] != "hi-1" || got[2] != "hi-2" {
		t.Fatalf("got %v, want [hi-0 hi-1 hi-2]", got)
	}
}

// TestBidiStream proves bidirectional streaming: the client streams N messages
// and half-closes; the server echoes each back (prefixed) as it arrives, then
// sees EOF and half-closes its side; the client receives N echoes then io.EOF.
func TestBidiStream(t *testing.T) {
	sh := func(method uint32, init []byte, s *Stream) {
		for {
			msg, err := s.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			_ = s.Send(append([]byte("echo:"), msg...))
		}
	}
	srv, err := ListenStream("tcp", "127.0.0.1:0", nil, sh)
	if err != nil {
		t.Fatalf("ListenStream: %v", err)
	}
	defer srv.Close()

	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	st, err := conn.OpenStream(1, nil)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := st.Send([]byte(fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if err := st.CloseSend(); err != nil {
		t.Fatalf("CloseSend: %v", err)
	}

	var got []string
	for {
		msg, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, string(msg))
	}
	if len(got) != 3 || got[0] != "echo:m0" || got[2] != "echo:m2" {
		t.Fatalf("got %v, want [echo:m0 echo:m1 echo:m2]", got)
	}
}

// TestStreamContextCancelOnConnDrop proves a stream handler that blocks on
// Stream.Context().Done() (an idle long-lived subscription) is released when the
// peer drops the connection — the leak fix. Without per-stream cancellation the
// handler would block forever (no frames arrive to error a Send/Recv).
func TestStreamContextCancelOnConnDrop(t *testing.T) {
	released := make(chan struct{})
	h := func(method uint32, init []byte, s *Stream) {
		<-s.Context().Done() // idle: no Send/Recv, only the context can free us
		close(released)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := ServeStream(ln, echoDispatch, h)
	defer srv.Close()

	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := conn.OpenStream(1, []byte("subscribe")); err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	// Drop the connection while the handler is idle.
	_ = conn.Close()

	select {
	case <-released:
	case <-time.After(3 * time.Second):
		t.Fatal("idle stream handler not released after peer dropped the connection (leak)")
	}
}
