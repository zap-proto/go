// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import "crypto/tls"

// PQTLSConfig returns a TLS 1.3 configuration that REQUIRES the
// X25519MLKEM768 hybrid post-quantum key exchange — "PQ X-Wing": X25519
// ECDH composed with ML-KEM-768, so the session key is safe unless BOTH the
// classical and the lattice problem fall. Because X25519MLKEM768 is the
// only offered curve, a peer that cannot do PQ fails the handshake rather
// than silently downgrading to classical-only.
//
// X25519MLKEM768 is native to Go 1.24+ crypto/tls — no CGO, no liboqs. base
// supplies certificates / roots (and any other fields); pass nil to start
// from an empty config. The returned config is safe to share across
// connections.
//
// Used by both [DialTLS] (client: set RootCAs or InsecureSkipVerify) and
// [ListenTLS] (server: set Certificates). The same PQ guarantee applies to
// the QUIC transport, which reuses this config.
func PQTLSConfig(base *tls.Config) *tls.Config {
	c := base.Clone()
	if c == nil {
		c = &tls.Config{}
	}
	c.MinVersion = tls.VersionTLS13
	c.CurvePreferences = []tls.CurveID{tls.X25519MLKEM768}
	return c
}

// DialTLS connects to addr over a PQ-secured TLS connection and returns a
// call-only [Conn]. conf should usually be wrapped with [PQTLSConfig] to
// pin X25519MLKEM768. The handshake is forced before returning so a
// curve-negotiation failure (peer cannot do PQ) surfaces here, not on the
// first Call.
func DialTLS(network, addr string, conf *tls.Config) (*Conn, error) {
	return DialServeTLS(network, addr, conf, nil)
}

// DialServeTLS is [DialTLS] plus an inbound Dispatch (bidirectional peer).
func DialServeTLS(network, addr string, conf *tls.Config, dispatch Dispatch) (*Conn, error) {
	nc, err := tls.Dial(network, addr, conf)
	if err != nil {
		return nil, err
	}
	if err := nc.Handshake(); err != nil {
		_ = nc.Close()
		return nil, err
	}
	return NewConn(nc, dispatch), nil
}

// ListenTLS binds addr on network and serves dispatch over PQ-secured TLS.
// conf must carry a server certificate; wrap it with [PQTLSConfig] to
// require X25519MLKEM768.
func ListenTLS(network, addr string, conf *tls.Config, dispatch Dispatch) (*Server, error) {
	if network == "unix" {
		_ = removeIfSocket(addr)
	}
	ln, err := tls.Listen(network, addr, conf)
	if err != nil {
		return nil, err
	}
	return Serve(ln, dispatch), nil
}

// TLS returns the negotiated TLS connection state, or nil if the connection
// is plaintext. Use it to confirm the PQ curve in use, e.g.
// `conn.TLS().testingOnlyCurveID` in tests or `conn.TLS().Version` /
// `CipherSuite` for audit logging.
func (c *Conn) TLS() *tls.ConnectionState {
	if tc, ok := c.nc.(*tls.Conn); ok {
		st := tc.ConnectionState()
		return &st
	}
	return nil
}
