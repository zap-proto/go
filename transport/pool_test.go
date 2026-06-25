// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"net"
	"sync/atomic"
	"testing"

	"github.com/zap-proto/go/rpc"
)

func TestPool_ReuseEvictClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := Serve(ln, echoDispatch)
	defer srv.Close()
	addr := srv.Addr().String()

	var dials atomic.Int64
	p := NewPool(func(a string) (*Conn, error) {
		dials.Add(1)
		return Dial("tcp", a)
	})

	// Reuse: two Gets return the same live conn after a single dial.
	c1, err := p.Get(addr)
	if err != nil {
		t.Fatalf("Get1: %v", err)
	}
	c2, err := p.Get(addr)
	if err != nil {
		t.Fatalf("Get2: %v", err)
	}
	if c1 != c2 {
		t.Fatal("Pool.Get returned a different conn for the same addr")
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1 (reuse)", got)
	}

	// Death: a dropped conn is detected on the next Get and replaced.
	_ = c1.Close()
	c3, err := p.Get(addr)
	if err != nil {
		t.Fatalf("Get3: %v", err)
	}
	if c3 == c1 {
		t.Fatal("Pool.Get returned the dead conn")
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("dials = %d, want 2 (redial after death)", got)
	}

	// Evict only removes the matching entry.
	p.Evict(addr, c1) // stale: c3 is cached now, so this is a no-op
	if c4, _ := p.Get(addr); c4 != c3 {
		t.Fatal("stale Evict wrongly dropped the live conn")
	}
	p.Evict(addr, c3) // current: drops it
	if c5, _ := p.Get(addr); c5 == c3 {
		t.Fatal("Evict did not drop the current conn")
	}
	if got := dials.Load(); got != 3 {
		t.Fatalf("dials = %d, want 3", got)
	}

	// Close closes everything.
	p.Close()
	c6, _ := p.Get(addr)
	if c6 == nil {
		t.Fatal("Get after Close should redial")
	}
	if got := dials.Load(); got != 4 {
		t.Fatalf("dials = %d, want 4 (redial after Close)", got)
	}
}

func TestPool_With(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := Serve(ln, echoDispatch)
	defer srv.Close()
	addr := srv.Addr().String()

	var dials atomic.Int64
	p := NewPool(func(a string) (*Conn, error) { dials.Add(1); return Dial("tcp", a) })

	// Two Withs reuse one conn; an echo round-trip proves the borrowed conn works.
	for i := 0; i < 2; i++ {
		if err := p.With(addr, func(c *Conn) error {
			resp, err := c.Call(rpc.BuildRequest(rpc.Call{Method: 1, PromiseID: c.NextPromiseID(), Payload: []byte("hi")}))
			if err != nil {
				return err
			}
			if string(resp.Body) != "hi" {
				t.Fatalf("echo = %q", resp.Body)
			}
			return nil
		}); err != nil {
			t.Fatalf("With: %v", err)
		}
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1 (With reuses)", got)
	}

	// A conn that dies inside fn is evicted, so the next With redials.
	_ = p.With(addr, func(c *Conn) error { _ = c.Close(); return ErrClosed })
	_ = p.With(addr, func(c *Conn) error { return nil })
	if got := dials.Load(); got != 2 {
		t.Fatalf("dials = %d, want 2 (redial after in-fn death)", got)
	}
}
