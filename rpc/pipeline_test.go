// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

// The pipelining tests model the canonical use case: call A authenticates and
// returns an opaque org token; call B (getResource) needs that token as its
// input. Pipelined, B sets Target = A's PromiseID and ships immediately — the
// server substitutes A's resolved token for B's payload before dispatching B,
// so B never waits for A's answer to round-trip back to the client.

const (
	mAuthenticate uint32 = 1 // () -> token
	mGetResource  uint32 = 2 // (token) -> "resource@<token>"
)

// authDispatch is a DispatchFunc: authenticate returns a fixed token; get
// returns a resource string keyed by the token it receives as payload. It
// records, in order, the payload every getResource call was dispatched with —
// the proof that the server fed A's result into B.
type authServer struct {
	mu       sync.Mutex
	token    string
	gotInput [][]byte // payloads getResource was dispatched with, in order
}

func (s *authServer) dispatch(envelope []byte) ([]byte, error) {
	call, err := ParseRequest(envelope)
	if err != nil {
		return nil, err
	}
	switch call.Method {
	case mAuthenticate:
		return BuildResponse(StatusOK, call.PromiseID, []byte(s.token)), nil
	case mGetResource:
		s.mu.Lock()
		s.gotInput = append(s.gotInput, append([]byte(nil), call.Payload...))
		s.mu.Unlock()
		return BuildResponse(StatusOK, call.PromiseID, []byte("resource@"+string(call.Payload))), nil
	default:
		return BuildResponse(StatusNotFound, call.PromiseID, nil), nil
	}
}

// TestPipelineResolvesTarget is the core end-to-end proof: B pipelines on A's
// answer via Target, the Pipeliner substitutes A's resolved token for B's
// payload before dispatch, and B's result reflects it — no round trip threads
// A's body back through the client.
func TestPipelineResolvesTarget(t *testing.T) {
	srv := &authServer{token: "org-7"}
	p := NewPipeliner(srv.dispatch)
	sess := NewSession()

	// A: authenticate, fresh PromiseID, Target = NoTarget.
	a := sess.Next()
	aResp, err := handle(t, p, sess.Origin(a, mAuthenticate, nil, nil))
	if err != nil {
		t.Fatalf("A handle: %v", err)
	}
	if got := string(aResp.Body); got != "org-7" {
		t.Fatalf("A body = %q, want org-7", got)
	}

	// B: getResource pipelined on A. Its payload is supplied server-side by
	// A's resolved token — the client sends nil.
	b := sess.Next()
	bResp, err := handle(t, p, sess.Pipeline(b, a, mGetResource, nil, nil))
	if err != nil {
		t.Fatalf("B handle: %v", err)
	}
	if got := string(bResp.Body); got != "resource@org-7" {
		t.Errorf("B body = %q, want resource@org-7 (A's token fed into B)", got)
	}

	// The server must have dispatched B with A's token as the payload.
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.gotInput) != 1 || !bytes.Equal(srv.gotInput[0], []byte("org-7")) {
		t.Errorf("getResource dispatched with %q, want [org-7]", srv.gotInput)
	}
}

// TestPipelineQueuesUntilResolved proves server-side queuing: B arrives BEFORE
// A and must park inside the Pipeliner until A resolves, then complete with
// A's result. We launch B first on its own goroutine, confirm it is still
// blocked, then handle A — which releases B.
func TestPipelineQueuesUntilResolved(t *testing.T) {
	srv := &authServer{token: "org-42"}
	p := NewPipeliner(srv.dispatch)
	sess := NewSession()

	a := sess.Next()
	b := sess.Next()

	bDone := make(chan Response, 1)
	go func() {
		// B references A's PromiseID, but A has NOT been handled yet — B must
		// block inside Handle until A resolves.
		resp, err := handle(t, p, sess.Pipeline(b, a, mGetResource, nil, nil))
		if err != nil {
			t.Errorf("B handle: %v", err)
			close(bDone)
			return
		}
		bDone <- resp
	}()

	// Give B a chance to park. It must NOT complete before A is handled.
	select {
	case <-bDone:
		t.Fatal("B completed before A resolved — it was not queued on its Target")
	case <-time.After(50 * time.Millisecond):
		// expected: B is parked.
	}

	// Now resolve A; B must unblock with A's token fed in.
	if _, err := handle(t, p, sess.Origin(a, mAuthenticate, nil, nil)); err != nil {
		t.Fatalf("A handle: %v", err)
	}
	select {
	case resp, ok := <-bDone:
		if !ok {
			t.Fatal("B errored (see above)")
		}
		if got := string(resp.Body); got != "resource@org-42" {
			t.Errorf("B body = %q, want resource@org-42", got)
		}
	case <-time.After(time.Second):
		t.Fatal("B never resolved after A was handled")
	}
}

// TestPipelineTargetFailurePropagates proves a dependent whose Target answered
// non-OK is refused (StatusBadRequest), not hung: there is no result to
// pipeline on. Here authenticate is mapped to an ordinal the server rejects.
func TestPipelineTargetFailurePropagates(t *testing.T) {
	// A server that fails authentication with StatusUnauthorized.
	dispatch := func(envelope []byte) ([]byte, error) {
		call, err := ParseRequest(envelope)
		if err != nil {
			return nil, err
		}
		if call.Method == mAuthenticate {
			return BuildResponse(StatusUnauthorized, call.PromiseID, nil), nil
		}
		return BuildResponse(StatusOK, call.PromiseID, []byte("resource@"+string(call.Payload))), nil
	}
	p := NewPipeliner(dispatch)
	sess := NewSession()

	a := sess.Next()
	aResp, err := handle(t, p, sess.Origin(a, mAuthenticate, nil, nil))
	if err != nil {
		t.Fatalf("A handle: %v", err)
	}
	if aResp.Status != StatusUnauthorized {
		t.Fatalf("A status = %d, want Unauthorized", aResp.Status)
	}

	b := sess.Next()
	bResp, err := handle(t, p, sess.Pipeline(b, a, mGetResource, nil, nil))
	if err != nil {
		t.Fatalf("B handle: %v", err)
	}
	if bResp.Status != StatusBadRequest {
		t.Errorf("B status = %d, want BadRequest (target never resolves)", bResp.Status)
	}
}

// TestPipelineQueuedFailurePropagates is the queued twin of the above: B parks
// before A, then A fails — B must wake with StatusBadRequest, not hang.
func TestPipelineQueuedFailurePropagates(t *testing.T) {
	dispatch := func(envelope []byte) ([]byte, error) {
		call, _ := ParseRequest(envelope)
		if call.Method == mAuthenticate {
			return BuildResponse(StatusForbidden, call.PromiseID, nil), nil
		}
		return BuildResponse(StatusOK, call.PromiseID, nil), nil
	}
	p := NewPipeliner(dispatch)
	sess := NewSession()
	a := sess.Next()
	b := sess.Next()

	bDone := make(chan Response, 1)
	go func() {
		resp, err := handle(t, p, sess.Pipeline(b, a, mGetResource, nil, nil))
		if err != nil {
			t.Errorf("B handle: %v", err)
		}
		bDone <- resp
	}()
	time.Sleep(20 * time.Millisecond) // let B park

	if _, err := handle(t, p, sess.Origin(a, mAuthenticate, nil, nil)); err != nil {
		t.Fatalf("A handle: %v", err)
	}
	select {
	case resp := <-bDone:
		if resp.Status != StatusBadRequest {
			t.Errorf("queued B status = %d, want BadRequest after A failed", resp.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("queued B never woke after A failed")
	}
}

// TestPipelineFinishDropsAnswer proves Finish bounds the table: once a promise
// is Finished, a later dependent targeting it is refused (BadRequest), exactly
// like a never-resolved target, instead of finding a stale cached answer.
func TestPipelineFinishDropsAnswer(t *testing.T) {
	srv := &authServer{token: "org-9"}
	p := NewPipeliner(srv.dispatch)
	sess := NewSession()

	a := sess.Next()
	if _, err := handle(t, p, sess.Origin(a, mAuthenticate, nil, nil)); err != nil {
		t.Fatalf("A handle: %v", err)
	}
	// Before Finish: the dependent resolves.
	b := sess.Next()
	if resp, err := handle(t, p, sess.Pipeline(b, a, mGetResource, nil, nil)); err != nil {
		t.Fatalf("B handle: %v", err)
	} else if string(resp.Body) != "resource@org-9" {
		t.Fatalf("B body = %q, want resource@org-9", resp.Body)
	}

	// After Finish: A's answer is gone; a new dependent on A is refused.
	p.Finish(a.ID)
	c := sess.Next()
	resp, err := handle(t, p, sess.Pipeline(c, a, mGetResource, nil, nil))
	if err != nil {
		t.Fatalf("C handle: %v", err)
	}
	if resp.Status != StatusBadRequest {
		t.Errorf("C status = %d, want BadRequest after A Finished", resp.Status)
	}
}

// TestPipelineChainOfThree proves a chain deeper than two resolves: C
// pipelines on B which pipelines on A, all queued before A resolves. The
// server feeds A's answer into B and B's answer into C, each in turn. The
// chain server prefixes "->" so the depth is visible in the final result.
func TestPipelineChainOfThree(t *testing.T) {
	// method 1 seeds "a"; method 2 appends ">" + its payload-derived step.
	dispatch := func(envelope []byte) ([]byte, error) {
		call, _ := ParseRequest(envelope)
		switch call.Method {
		case 1:
			return BuildResponse(StatusOK, call.PromiseID, []byte("a")), nil
		case 2:
			return BuildResponse(StatusOK, call.PromiseID, append(append([]byte(nil), call.Payload...), '>')), nil
		default:
			return BuildResponse(StatusNotFound, call.PromiseID, nil), nil
		}
	}
	p := NewPipeliner(dispatch)
	sess := NewSession()
	a, b, c := sess.Next(), sess.Next(), sess.Next()

	// Queue C (on B) and B (on A) BEFORE A — both must park, then cascade.
	cDone := make(chan Response, 1)
	bDone := make(chan Response, 1)
	go func() { r, _ := handle(t, p, sess.Pipeline(c, b, 2, nil, nil)); cDone <- r }()
	go func() { r, _ := handle(t, p, sess.Pipeline(b, a, 2, nil, nil)); bDone <- r }()
	time.Sleep(30 * time.Millisecond) // let B and C park

	if _, err := handle(t, p, sess.Origin(a, 1, nil, nil)); err != nil {
		t.Fatalf("A handle: %v", err)
	}
	bResp := <-bDone
	cResp := <-cDone
	if got := string(bResp.Body); got != "a>" {
		t.Errorf("B body = %q, want a>", got)
	}
	if got := string(cResp.Body); got != "a>>" {
		t.Errorf("C body = %q, want a>> (chained A->B->C)", got)
	}
}

// TestPipelineFinishWakesParkedDependent proves that Finishing a Target with a
// dependent already parked on it wakes that dependent with a refusal instead
// of hanging it forever (the answer is gone and will never be re-produced).
func TestPipelineFinishWakesParkedDependent(t *testing.T) {
	srv := &authServer{token: "org-3"}
	p := NewPipeliner(srv.dispatch)
	sess := NewSession()
	a := sess.Next()
	b := sess.Next()

	bDone := make(chan Response, 1)
	go func() {
		resp, err := handle(t, p, sess.Pipeline(b, a, mGetResource, nil, nil))
		if err != nil {
			t.Errorf("B handle: %v", err)
		}
		bDone <- resp
	}()
	time.Sleep(20 * time.Millisecond) // let B park on A (A never originated)

	p.Finish(a.ID) // A will never produce an answer — refuse B.
	select {
	case resp := <-bDone:
		if resp.Status != StatusBadRequest {
			t.Errorf("parked B status = %d, want BadRequest after A Finished", resp.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("parked B never woke after A Finished")
	}
}

// TestPipelineWireEncoding proves the dependent call's Target rides on the
// wire as the prior call's PromiseID (byte-level), and a non-pipelined call
// carries NoTarget — the encoding is unchanged and byte-compatible across
// runtimes.
func TestPipelineWireEncoding(t *testing.T) {
	sess := NewSession()
	a := sess.Next()
	b := sess.Next()
	if a.ID == NoTarget || b.ID == NoTarget || a.ID == b.ID {
		t.Fatalf("PromiseIDs must be unique and non-zero: a=%d b=%d", a.ID, b.ID)
	}

	// Originating call A: Target field (@8) must be NoTarget on the wire.
	aEnv := BuildRequest(sess.Origin(a, mAuthenticate, nil, nil))
	if got := wireTarget(t, aEnv); got != NoTarget {
		t.Errorf("A wire Target = %d, want NoTarget(0)", got)
	}
	if got := wirePromiseID(t, aEnv); got != a.ID {
		t.Errorf("A wire PromiseID = %d, want %d", got, a.ID)
	}

	// Dependent call B: Target field (@8) must equal A's PromiseID on the wire.
	bEnv := BuildRequest(sess.Pipeline(b, a, mGetResource, nil, nil))
	if got := wireTarget(t, bEnv); got != a.ID {
		t.Errorf("B wire Target = %d, want A.ID %d", got, a.ID)
	}
	if got := wirePromiseID(t, bEnv); got != b.ID {
		t.Errorf("B wire PromiseID = %d, want %d", got, b.ID)
	}

	// And it round-trips through the decoder unchanged.
	call, err := ParseRequest(bEnv)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if call.Target != a.ID || call.PromiseID != b.ID || call.Method != mGetResource {
		t.Errorf("decoded B = %+v, want Target=%d PromiseID=%d Method=%d", call, a.ID, b.ID, mGetResource)
	}
}

// --- helpers ----------------------------------------------------------------

// handle ships one Call through the Pipeliner and returns the parsed Response.
func handle(t *testing.T, p *Pipeliner, c Call) (Response, error) {
	t.Helper()
	respBytes, err := p.Handle(BuildRequest(c))
	if err != nil {
		return Response{}, err
	}
	return ParseResponse(respBytes)
}

// wireTarget / wirePromiseID read the request struct's Target(@8) /
// PromiseID(@4) directly from the encoded message, proving the on-wire bytes
// (not just the in-memory Call) carry the pipelining reference.
func wireTarget(t *testing.T, env []byte) uint32 {
	t.Helper()
	return wireField(t, env, reqTargetOff)
}

func wirePromiseID(t *testing.T, env []byte) uint32 {
	t.Helper()
	return wireField(t, env, reqPromiseIDOff)
}

func wireField(t *testing.T, env []byte, off int) uint32 {
	t.Helper()
	// Validate framing the same way a peer would before trusting offsets.
	if _, err := ParseRequest(env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	// Root object byte offset lives in the header at [8:12]; a field at struct
	// offset `off` is at byte rootOffset+off. Read the u32 there directly so we
	// assert the on-wire bytes, not an in-memory struct.
	root := int(binary.LittleEndian.Uint32(env[8:12]))
	pos := root + off
	if pos+4 > len(env) {
		t.Fatalf("field offset %d out of range (msg len %d)", pos, len(env))
	}
	return binary.LittleEndian.Uint32(env[pos : pos+4])
}
