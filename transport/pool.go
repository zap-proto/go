// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import "sync"

// Pool caches and reuses [Conn]s keyed by address. A Conn is safe for concurrent
// use — many goroutines can [Conn.Call] and [Conn.OpenStream] on one — so reusing
// it across requests avoids a fresh dial (and, over TLS/QUIC, a fresh handshake;
// over PQ-TLS, a fresh X25519MLKEM768 exchange) on every call. A conn that the
// peer dropped is detected via [Conn.IsClosed] on the next [Pool.Get] and
// replaced by a fresh dial; callers evict eagerly with [Pool.Evict] when a Call
// returns [ErrClosed].
//
// The dial function is injected, so the caller owns the transport choice —
// plaintext [Dial], PQ-TLS [DialTLS]+[PQTLSConfig], QUIC — and the network/addr
// shape. Pool itself is transport-agnostic. Safe for concurrent use.
type Pool struct {
	dial func(addr string) (Conn, error)

	mu    sync.Mutex
	conns map[string]Conn
}

// NewPool returns a Pool that dials new connections with dial.
func NewPool(dial func(addr string) (Conn, error)) *Pool {
	return &Pool{dial: dial, conns: make(map[string]Conn)}
}

// Get returns a live connection to addr, dialing and caching one if none is
// live. Concurrent Gets for the same addr that race a dial keep one winner and
// close the loser, so at most one conn per addr is cached.
func (p *Pool) Get(addr string) (Conn, error) {
	p.mu.Lock()
	if c := p.conns[addr]; c != nil && !c.IsClosed() {
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()

	nc, err := p.dial(addr)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if c := p.conns[addr]; c != nil && !c.IsClosed() {
		p.mu.Unlock()
		_ = nc.Close()
		return c, nil
	}
	p.conns[addr] = nc
	p.mu.Unlock()
	return nc, nil
}

// With gets (or dials) a pooled conn for addr, runs fn with it, and evicts the
// conn if it died during fn — the canonical "borrow a connection for one logical
// operation" pattern. fn may issue any number of Calls and OpenStreams on the
// shared conn; it must not Close it (the Pool owns the conn's lifetime). A
// per-service typed helper is then a one-liner, e.g.
//
//	func WithFiler(addr string, fn func(FilerClient) error) error {
//	    return filerPool.With(addr, func(c transport.Conn) error { return fn(NewFilerClient(c)) })
//	}
func (p *Pool) With(addr string, fn func(Conn) error) error {
	conn, err := p.Get(addr)
	if err != nil {
		return err
	}
	err = fn(conn)
	if conn.IsClosed() {
		p.Evict(addr, conn)
	}
	return err
}

// Evict drops conn for addr, but only if it is still the cached entry — so a
// late Evict of a conn already replaced by a healthy redial is a no-op. Call it
// when a request on conn fails with [ErrClosed].
func (p *Pool) Evict(addr string, conn Conn) {
	p.mu.Lock()
	if p.conns[addr] == conn {
		delete(p.conns, addr)
	}
	p.mu.Unlock()
}

// Close closes every pooled connection and empties the pool. The Pool may be
// reused afterward (a subsequent Get redials).
func (p *Pool) Close() {
	p.mu.Lock()
	for addr, c := range p.conns {
		_ = c.Close()
		delete(p.conns, addr)
	}
	p.mu.Unlock()
}
