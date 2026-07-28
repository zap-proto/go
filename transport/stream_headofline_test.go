// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zap-proto/go/rpc"
)

// TestSlowStreamDoesNotStarveTheConnection pins the invariant that makes a
// pooled connection usable: inbound stream frames are delivered from the
// connection's single read loop, so delivery must never block on the consumer.
//
// It used to. Frames were handed to a 16-slot channel with a blocking send, so
// a consumer slower than its producer filled the buffer, stalled the read loop,
// and with it every other stream and every unary reply on that connection. In
// production one slow ListEntries on a shared filer connection froze every S3
// request in the process — and unauthenticated requests kept working, because
// they are rejected before they ever reach the filer, which made it look like
// an auth bug.
func TestSlowStreamDoesNotStarveTheConnection(t *testing.T) {
	const frames = 200 // well past the old 16-slot buffer

	dispatch := func(env []byte) ([]byte, error) {
		call, err := rpc.ParseRequest(env)
		if err != nil {
			return nil, err
		}
		return rpc.BuildResponse(rpc.StatusOK, call.PromiseID, []byte("pong")), nil
	}
	stream := func(method uint32, init []byte, s Stream) {
		for i := 0; i < frames; i++ {
			if err := s.Send([]byte(fmt.Sprintf("row-%d", i))); err != nil {
				return
			}
		}
	}

	srv, err := ListenStream("tcp", "127.0.0.1:0", dispatch, stream)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	// ONE connection shared by both callers, as transport.Pool hands out.
	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	st, err := conn.OpenStream(1, nil)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	// A deliberately slow consumer: it cannot keep up with the producer.
	go func() {
		for {
			if _, err := st.Recv(); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Give the producer time to run far ahead of that consumer.
	time.Sleep(200 * time.Millisecond)

	// An unrelated unary call on the same connection must still be answered.
	sess := rpc.NewSession()
	p := sess.Next()
	env := rpc.BuildRequest(rpc.Call{Method: 2, PromiseID: p.ID, Target: rpc.NoTarget})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := conn.CallContext(ctx, env)
	if err != nil {
		t.Fatalf("unary call starved by the slow stream after %v: %v", time.Since(start), err)
	}
	if got := string(resp.Body); got != "pong" {
		t.Fatalf("body = %q, want %q", got, "pong")
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("unary call took %v behind a slow stream; the read loop is still blocking", el)
	}
}

// TestStreamOverflowFailsOnlyThatStream checks the bound: a consumer that never
// reads must not pin memory without limit, and when its queue overflows the
// failure has to stay contained to that stream — the connection keeps serving.
func TestStreamOverflowFailsOnlyThatStream(t *testing.T) {
	dispatch := func(env []byte) ([]byte, error) {
		call, err := rpc.ParseRequest(env)
		if err != nil {
			return nil, err
		}
		return rpc.BuildResponse(rpc.StatusOK, call.PromiseID, []byte("pong")), nil
	}
	stream := func(method uint32, init []byte, s Stream) {
		for i := 0; i < maxStreamQueue+64; i++ {
			if err := s.Send([]byte("x")); err != nil {
				return
			}
		}
	}

	srv, err := ListenStream("tcp", "127.0.0.1:0", dispatch, stream)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	st, err := conn.OpenStream(1, nil)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	// Never consume; wait for the producer to overrun the queue bound.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ms := st.(*muxStream)
		ms.mu.Lock()
		over := ms.overflow
		ms.mu.Unlock()
		if over {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The connection must still answer other work.
	sess := rpc.NewSession()
	p := sess.Next()
	env := rpc.BuildRequest(rpc.Call{Method: 2, PromiseID: p.ID, Target: rpc.NoTarget})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := conn.CallContext(ctx, env); err != nil {
		t.Fatalf("connection unusable after a stream overflowed: %v", err)
	}

	// And the overflowed stream reports it rather than hanging, once drained.
	for {
		_, err := st.Recv()
		if err == nil {
			continue // drain what was queued before the overflow
		}
		if !errors.Is(err, ErrStreamOverflow) {
			t.Fatalf("Recv after overflow = %v, want ErrStreamOverflow", err)
		}
		break
	}
}
