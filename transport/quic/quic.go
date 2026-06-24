// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package quic carries ZAP RPC over QUIC (RFC 9000) — multiplexed,
// 0-RTT-capable, connection-migrating transport — behind the exact same
// [transport.Conn] / [transport.Dispatch] contract as the TCP/Unix
// transport. A generated client takes the returned *transport.Conn with no
// change; the only difference from [transport.Dial] is the wire underneath.
//
// Every QUIC connection is PQ-secured: the TLS 1.3 handshake QUIC mandates
// is pinned to the X25519MLKEM768 hybrid (PQ X-Wing) via
// [transport.PQTLSConfig], so a non-PQ peer fails the handshake.
//
// This is the only package in the module that pulls github.com/quic-go/
// quic-go (pure Go, no CGO); import it only when you want QUIC. The
// TCP/Unix transport stays dependency-free.
package quic

import (
	"context"
	"crypto/tls"
	"net"

	qgo "github.com/quic-go/quic-go"
	"github.com/zap-proto/go/transport"
)

// alpn is the ALPN protocol id QUIC requires; both ends must agree.
const alpn = "zap/1"

// pqTLS applies the PQ X-Wing config and the ZAP ALPN, leaving caller-set
// certs/roots intact.
func pqTLS(conf *tls.Config) *tls.Config {
	c := transport.PQTLSConfig(conf)
	if len(c.NextProtos) == 0 {
		c.NextProtos = []string{alpn}
	}
	return c
}

// streamConn adapts a QUIC stream to io.ReadWriteCloser for
// [transport.NewConn], holding the parent connection alive and tearing it
// down when the RPC conn closes. One bidirectional stream per connection
// carries the framed envelopes (multiplexing per-call streams is a later
// optimisation; the contract is unchanged either way).
type streamConn struct {
	*qgo.Stream
	conn *qgo.Conn
}

func (s streamConn) Close() error {
	_ = s.Stream.Close() // half-close the send side
	return s.conn.CloseWithError(0, "")
}

// Dial opens a PQ-secured QUIC connection to addr and returns a call-only
// [transport.Conn] over its first bidirectional stream. conf supplies the
// client's roots (or InsecureSkipVerify in tests); the PQ curve + ALPN are
// applied here.
func Dial(ctx context.Context, addr string, conf *tls.Config) (*transport.Conn, error) {
	qc, err := qgo.DialAddr(ctx, addr, pqTLS(conf), nil)
	if err != nil {
		return nil, err
	}
	st, err := qc.OpenStreamSync(ctx)
	if err != nil {
		_ = qc.CloseWithError(0, "")
		return nil, err
	}
	return transport.NewConn(streamConn{Stream: st, conn: qc}, nil), nil
}

// Server accepts PQ-secured QUIC connections and serves dispatch on the
// first bidirectional stream of each.
type Server struct {
	ln       *qgo.Listener
	dispatch transport.Dispatch
	ctx      context.Context
	cancel   context.CancelFunc
}

// Listen binds addr (UDP) and serves dispatch over PQ-secured QUIC. conf
// must carry a server certificate.
func Listen(addr string, conf *tls.Config, dispatch transport.Dispatch) (*Server, error) {
	ln, err := qgo.ListenAddr(addr, pqTLS(conf), nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{ln: ln, dispatch: dispatch, ctx: ctx, cancel: cancel}
	go s.acceptLoop()
	return s, nil
}

func (s *Server) acceptLoop() {
	for {
		qc, err := s.ln.Accept(s.ctx)
		if err != nil {
			return // listener closed
		}
		go s.serveConn(qc)
	}
}

func (s *Server) serveConn(qc *qgo.Conn) {
	// The client opens its stream lazily — AcceptStream returns once the
	// first request frame arrives.
	st, err := qc.AcceptStream(s.ctx)
	if err != nil {
		_ = qc.CloseWithError(0, "")
		return
	}
	transport.NewConn(streamConn{Stream: st, conn: qc}, s.dispatch)
}

// Addr is the listener's UDP address (useful with ":0").
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Close stops accepting and closes the listener.
func (s *Server) Close() error {
	s.cancel()
	return s.ln.Close()
}
