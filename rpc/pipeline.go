// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"fmt"
	"sync"
)

// Promise pipelining (the ONE canonical ZAP model).
//
// A call carries a caller-assigned PromiseID (the id its answer resolves
// to). A *dependent* call sets Target = a prior call's PromiseID, meaning
// "before you dispatch me, substitute the resolved Body of the call that
// answered to that PromiseID as my Payload". The result of A is the input
// to B, so B never waits for A's Body to round-trip back to the caller —
// both calls ship back-to-back and the server chains them.
//
// Two pieces implement it, each in one place (orthogonal, no braiding):
//
//   - Session (client side) allocates PromiseIDs and stamps Target onto a
//     dependent Call, so a caller can reference a prior in-flight answer.
//   - Pipeliner (server side) is the promise table: it resolves Target
//     before dispatch (substituting the resolved Body for the dependent
//     Payload), records every OK answer under its PromiseID, and queues a
//     dependent call whose Target has not resolved yet until it does.
//
// Both are transport-agnostic: they operate on Call / Response and the raw
// dispatch function (the generated Dispatch<Iface>), so the exact same
// model works over the in-process loopback Channel and over a real TCP
// connection without change. The wire envelope is unchanged — Target has
// always been field @8; NoTarget (0) is a non-pipelined call, so a
// Pipeliner and a plain dispatcher are byte-compatible on the wire and a
// non-pipelining peer (any other language runtime) interoperates by simply
// sending Target = NoTarget.

// DispatchFunc is the generated server entry point (Dispatch<Iface>): it
// decodes a Call envelope, routes by ordinal to a handler, and returns the
// response envelope bytes. A Pipeliner wraps one of these.
type DispatchFunc func(envelope []byte) ([]byte, error)

// ErrUnresolvedTarget is returned when a dependent call names a Target that
// can never resolve on this session — it was never in flight, or its
// answer already failed (a non-OK status produces no usable result to
// pipeline on). Surfaced to the client as StatusBadRequest.
var ErrUnresolvedTarget = fmt.Errorf("rpc: pipeline target never resolves")

// Pipeliner is a server-side promise table for one session (one transport
// connection). It serializes dispatch (matching a ZAP connection's strict
// FIFO handler) while resolving Target references and queuing dependents
// whose target has not yet resolved.
//
// Lifecycle: construct with NewPipeliner(dispatch); feed each inbound
// request envelope to Handle, which returns the response envelope (or holds
// the dependent until its target resolves and then returns its response).
// A Pipeliner is safe for concurrent Handle calls — a transport that reads
// frames on multiple goroutines may call it from each.
type Pipeliner struct {
	dispatch DispatchFunc

	mu       sync.Mutex
	resolved map[uint32][]byte         // PromiseID -> resolved OK Body
	failed   map[uint32]struct{}       // PromiseID -> answered non-OK (no result)
	finished map[uint32]struct{}       // PromiseID -> Finished (answer dropped)
	waiters  map[uint32][]*pendingCall // Target -> calls queued on it
}

// pendingCall is a dependent call parked until its Target resolves. done is
// closed once result is set (the dependent's own response envelope).
type pendingCall struct {
	call   Call
	result []byte
	err    error
	done   chan struct{}
}

// NewPipeliner returns a Pipeliner that routes resolved calls through
// dispatch.
func NewPipeliner(dispatch DispatchFunc) *Pipeliner {
	return &Pipeliner{
		dispatch: dispatch,
		resolved: make(map[uint32][]byte),
		failed:   make(map[uint32]struct{}),
		finished: make(map[uint32]struct{}),
		waiters:  make(map[uint32][]*pendingCall),
	}
}

// Handle processes one inbound request envelope and returns its response
// envelope. A request with Target = NoTarget dispatches straight through.
// Otherwise the request is a dependent call and its Target decides what
// happens:
//
//   - resolved (OK): the resolved Body is substituted for the request's
//     Payload and it dispatches immediately.
//   - failed (non-OK) or Finished: refused with StatusBadRequest — the
//     Target can never produce a result to pipeline on.
//   - unknown: parked until a later Handle on the same Pipeliner resolves
//     it (the dependent legitimately arrived before its origin), or a
//     Finish on the Target refuses it.
func (p *Pipeliner) Handle(envelope []byte) ([]byte, error) {
	call, err := ParseRequest(envelope)
	if err != nil {
		return nil, err
	}
	return p.handleCall(call)
}

func (p *Pipeliner) handleCall(call Call) ([]byte, error) {
	if call.Target == NoTarget {
		return p.dispatchAndRecord(call)
	}

	// Dependent call: resolve, refuse, or park under its Target.
	p.mu.Lock()
	if body, ok := p.resolved[call.Target]; ok {
		p.mu.Unlock()
		call.Payload = body
		return p.dispatchAndRecord(call)
	}
	// A Target that answered non-OK, or was already Finished, can never
	// resolve — refuse rather than park forever.
	if _, bad := p.failed[call.Target]; bad {
		p.mu.Unlock()
		return BuildResponse(StatusBadRequest, call.PromiseID, nil), nil
	}
	if _, done := p.finished[call.Target]; done {
		p.mu.Unlock()
		return BuildResponse(StatusBadRequest, call.PromiseID, nil), nil
	}
	// Unknown Target: assume its originating call is still in flight (the
	// dependent legitimately arrived first) and park until a future Handle
	// resolves it. Finish on the Target wakes a parked dependent with a
	// refusal so it never hangs.
	pc := &pendingCall{call: call, done: make(chan struct{})}
	p.waiters[call.Target] = append(p.waiters[call.Target], pc)
	p.mu.Unlock()

	<-pc.done
	return pc.result, pc.err
}

// Finish drops the cached answer for id once the client knows no further call
// will pipeline on it (the ZAP analogue of capnp's Finish message). Calling
// Finish is optional: without it, a Pipeliner retains each OK answer for the
// session's lifetime so a dependent that arrives after its target resolves
// still finds it. A long-lived connection that pipelines heavily should Finish
// each promise it is done with to bound the table.
//
// After Finish, id is terminal: any dependent that targets it — whether
// already parked or arriving later — is refused (StatusBadRequest) rather
// than hung, since the answer is gone and will never be re-produced.
func (p *Pipeliner) Finish(id uint32) {
	p.mu.Lock()
	delete(p.resolved, id)
	delete(p.failed, id)
	p.finished[id] = struct{}{}
	woken := p.waiters[id]
	delete(p.waiters, id)
	p.mu.Unlock()
	for _, pc := range woken {
		pc.result = BuildResponse(StatusBadRequest, pc.call.PromiseID, nil)
		close(pc.done)
	}
}

// dispatchAndRecord runs one resolved call through dispatch, records its OK
// answer under its PromiseID (so later dependents resolve), and releases any
// dependents parked on this PromiseID.
func (p *Pipeliner) dispatchAndRecord(call Call) ([]byte, error) {
	respBytes, err := p.dispatch(BuildRequest(call))
	if err != nil {
		// A transport-level dispatch failure poisons this PromiseID: any
		// dependent parked on it can never resolve.
		p.poison(call.PromiseID, err)
		return nil, err
	}
	resp, perr := ParseResponse(respBytes)
	if perr != nil {
		p.poison(call.PromiseID, perr)
		return nil, perr
	}
	p.record(call.PromiseID, resp)
	return respBytes, nil
}

// record stores an answer under id and wakes every dependent parked on it.
// An OK answer caches its Body (the value future dependents pipeline on); a
// non-OK answer marks id failed so its dependents are refused, not hung.
func (p *Pipeliner) record(id uint32, resp Response) {
	p.mu.Lock()
	var woken []*pendingCall
	if resp.Status == StatusOK {
		// Copy the body: respBytes aliases the dispatch buffer, but a parked
		// dependent reuses it as its Payload past this call's lifetime.
		body := append([]byte(nil), resp.Body...)
		p.resolved[id] = body
		woken = p.waiters[id]
		delete(p.waiters, id)
		p.mu.Unlock()
		for _, pc := range woken {
			pc.call.Payload = body
			pc.result, pc.err = p.dispatchAndRecord(pc.call)
			close(pc.done)
		}
		return
	}
	p.failed[id] = struct{}{}
	woken = p.waiters[id]
	delete(p.waiters, id)
	p.mu.Unlock()
	for _, pc := range woken {
		pc.result = BuildResponse(StatusBadRequest, pc.call.PromiseID, nil)
		close(pc.done)
	}
}

// poison wakes every dependent parked on id with err — used when dispatch
// itself fails (not a normal non-OK answer).
func (p *Pipeliner) poison(id uint32, err error) {
	p.mu.Lock()
	p.failed[id] = struct{}{}
	woken := p.waiters[id]
	delete(p.waiters, id)
	p.mu.Unlock()
	for _, pc := range woken {
		pc.err = err
		close(pc.done)
	}
}

// --- client-side origination ------------------------------------------------

// Session is the client half of pipelining: a monotonic PromiseID allocator
// scoped to one transport connection. The first call of a pipeline takes a
// fresh PromiseID via Next; a dependent call sets Target to that PromiseID,
// so the two ship back-to-back and the server's Pipeliner chains them.
//
// A Promise is a typed handle to a not-yet-resolved answer: pass it as the
// Target of the next Call instead of threading raw u32s by hand. PromiseIDs
// must be unique and non-zero within a session (0 is NoTarget); Session
// guarantees both.
type Session struct {
	mu   sync.Mutex
	next uint32
}

// NewSession returns a Session whose first allocated PromiseID is 1.
func NewSession() *Session {
	return &Session{}
}

// Promise is a handle to the answer of an in-flight call. Use its ID as the
// Target of a dependent Call to pipeline on it.
type Promise struct {
	// ID is the PromiseID the originating call's answer resolves to (never
	// NoTarget). A dependent call sets Target = ID.
	ID uint32
}

// Next allocates a fresh, unique, non-zero PromiseID for a new call and
// returns a Promise handle to its eventual answer.
func (s *Session) Next() Promise {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	if s.next == NoTarget { // wrapped past 2^32-1 back to 0 — skip it.
		s.next++
	}
	return Promise{ID: s.next}
}

// Origin builds the originating Call of a pipeline: it carries a fresh
// PromiseID (from p) and Target = NoTarget. cap and payload are this call's
// own arguments. The returned Call is ready for BuildRequest.
func (s *Session) Origin(p Promise, method uint32, cap, payload []byte) Call {
	return Call{
		Method:    method,
		PromiseID: p.ID,
		Target:    NoTarget,
		Cap:       cap,
		Payload:   payload,
	}
}

// Pipeline builds a dependent Call that pipelines on target's answer: it
// carries its own fresh PromiseID (from p) and Target = target.ID. The
// server substitutes target's resolved Body for this call's Payload before
// dispatch, so payload here is only the part of the request NOT supplied by
// the upstream answer (often nil — the whole input is the upstream result).
func (s *Session) Pipeline(p, target Promise, method uint32, cap, payload []byte) Call {
	return Call{
		Method:    method,
		PromiseID: p.ID,
		Target:    target.ID,
		Cap:       cap,
		Payload:   payload,
	}
}
