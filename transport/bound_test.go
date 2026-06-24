// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zap-proto/go/rpc"
)

// TestInboundDispatchBounded proves the read loop never runs more than
// maxInFlight inbound dispatches at once, even when a peer pipelines far more
// requests than that. Without the bound (M-1) a single socket could spawn one
// goroutine — and one handler run — per buffered frame, an unbounded
// resource-exhaustion amplifier.
func TestInboundDispatchBounded(t *testing.T) {
	var inflight, peak atomic.Int32
	gate := make(chan struct{})
	dispatched := make(chan struct{}, maxInFlight*2)

	dispatch := func(env []byte) ([]byte, error) {
		n := inflight.Add(1)
		for { // record the high-water mark
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		dispatched <- struct{}{}
		<-gate // hold the slot until the test releases
		inflight.Add(-1)
		call, _ := rpc.ParseRequest(env)
		return rpc.BuildResponse(rpc.StatusOK, call.PromiseID, nil), nil
	}

	srv, err := Listen("tcp", "127.0.0.1:0", dispatch)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	const total = maxInFlight + 64 // pipeline strictly more than the cap
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := rpc.BuildRequest(rpc.Call{
				Method:    1,
				PromiseID: conn.NextPromiseID(),
			})
			if _, err := conn.Call(req); err != nil {
				t.Errorf("Call: %v", err)
			}
		}()
	}

	// Block until maxInFlight handlers are simultaneously parked on the gate.
	// The read loop is now wedged trying to acquire the (maxInFlight+1)th slot,
	// so it cannot dispatch the remaining frames.
	for i := 0; i < maxInFlight; i++ {
		<-dispatched
	}
	if got := peak.Load(); got > maxInFlight {
		t.Fatalf("peak in-flight = %d, exceeds cap %d", got, maxInFlight)
	}

	close(gate) // release everyone; the remaining requests dispatch and finish
	wg.Wait()

	if got := peak.Load(); got != maxInFlight {
		t.Fatalf("peak in-flight = %d, want exactly the cap %d", got, maxInFlight)
	}
}
