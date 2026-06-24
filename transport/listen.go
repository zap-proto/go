// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"net"
	"sync"
)

// Server accepts ZAP-RPC connections on a listener and serves a [Dispatch]
// (a generated DispatchX bound to a handler) on each. Every accepted
// connection is a full [Conn], so a served peer may also issue calls back
// over the same socket.
type Server struct {
	ln       net.Listener
	dispatch Dispatch

	mu     sync.Mutex
	conns  map[*Conn]struct{}
	closed bool
}

// Listen binds addr on network ("tcp" or "unix") and serves dispatch on
// every accepted connection. For "unix", a stale socket file left by a
// previous crashed process is removed first (the listener would otherwise
// fail with EADDRINUSE).
//
//	srv, _ := transport.Listen("unix", sock, func(env []byte) ([]byte, error) {
//	    return mountwire.DispatchHanzoMount(handler, env)
//	})
//	defer srv.Close()
func Listen(network, addr string, dispatch Dispatch) (*Server, error) {
	if network == "unix" {
		// A leftover socket inode blocks bind; clear it. (Safe: a live
		// server holds the inode open, and we only remove the path, not an
		// in-use fd.)
		_ = removeIfSocket(addr)
	}
	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	return Serve(ln, dispatch), nil
}

// Serve serves dispatch on an already-bound listener (e.g. one handed in by
// the QUIC transport or a test). It takes ownership of ln.
func Serve(ln net.Listener, dispatch Dispatch) *Server {
	s := &Server{
		ln:       ln,
		dispatch: dispatch,
		conns:    make(map[*Conn]struct{}),
	}
	go s.acceptLoop()
	return s
}

// Addr is the listener's network address (useful with ":0" / a temp socket).
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

func (s *Server) acceptLoop() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return // listener closed
		}
		conn := NewConn(nc, s.dispatch)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
	}
}

// Close stops accepting and tears down every live connection.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	err := s.ln.Close()
	for _, c := range conns {
		_ = c.Close()
	}
	return err
}
