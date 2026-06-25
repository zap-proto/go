// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package quic carries ZAP RPC over QUIC (RFC 9000) NATIVELY, realising the
// [transport.Conn] / [transport.Stream] interfaces directly on QUIC streams
// rather than muxing ZAP frames over one byte pipe. Each ZAP operation gets
// its OWN QUIC stream, so concurrent calls never head-of-line-block each
// other — the muxed byte-pipe transport (TCP/Unix/TLS) serialises one writer;
// QUIC parallelises across streams natively. A generated client takes the
// returned [transport.Conn] with no change; only the wire underneath differs.
//
// Wire shape per QUIC stream (length-prefixed frames, no direction byte —
// the QUIC stream IS the correlation, so PromiseID muxing is unnecessary):
//
//		[ uint32 len ][ rpc envelope or message bytes ]
//
//	  - Unary Call: client opens a stream, writes one request frame, half-closes
//	    its send side, reads one response frame, done. Server accepts the stream,
//	    reads the first frame; a non-stream method dispatches and writes the
//	    single response frame, then closes.
//	  - Stream: the first frame is an rpc request (method + init). The server
//	    hands it to the [transport.StreamHandler]; thereafter both ends exchange
//	    message frames on the same QUIC stream. CloseSend = QUIC send-side close
//	    (peer Recv sees io.EOF); a 1-byte method discriminator on the open frame
//	    distinguishes unary from streaming.
//
// Every QUIC connection is PQ-secured: the TLS 1.3 handshake QUIC mandates is
// pinned to the X25519MLKEM768 hybrid (PQ X-Wing) via [transport.PQTLSConfig],
// so a non-PQ peer fails the handshake.
//
// This is the only package in the module that pulls github.com/quic-go/
// quic-go (pure Go, no CGO); import it only when you want QUIC. The TCP/Unix
// transport stays dependency-free.
package quic

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	qgo "github.com/quic-go/quic-go"
	"github.com/zap-proto/go/rpc"
	"github.com/zap-proto/go/transport"
)

// alpn is the ALPN protocol id QUIC requires; both ends must agree.
const alpn = "zap/1"

// maxFrame bounds a single frame so a corrupt length prefix cannot drive an
// unbounded allocation (matches the byte-pipe transport's bound).
const maxFrame = 64 << 20

// Frame openers. The first byte of the first frame on a QUIC stream tags
// whether it is a unary Call or a stream open; subsequent frames on a stream
// carry no tag (the QUIC stream already scopes them).
const (
	opCall   byte = 1 // unary request: one request frame -> one response frame
	opStream byte = 2 // stream open: rpc request (method+init), then messages
)

// pqTLS applies the PQ X-Wing config and the ZAP ALPN, leaving caller-set
// certs/roots intact.
func pqTLS(conf *tls.Config) *tls.Config {
	c := transport.PQTLSConfig(conf)
	if len(c.NextProtos) == 0 {
		c.NextProtos = []string{alpn}
	}
	return c
}

// quicConn realises [transport.Conn] natively over a *quic.Conn: every Call
// and OpenStream opens a fresh QUIC stream, so concurrent operations run on
// independent streams with no head-of-line blocking.
type quicConn struct {
	qc        *qgo.Conn
	promiseID atomic.Uint32

	closeOnce sync.Once
	closed    chan struct{}
}

func newQUICConn(qc *qgo.Conn) *quicConn {
	return &quicConn{qc: qc, closed: make(chan struct{})}
}

// Call opens a fresh QUIC stream, writes one request frame, half-closes the
// send side, and reads the single response frame. Concurrent Calls use
// concurrent streams — no head-of-line blocking.
func (c *quicConn) Call(envelope []byte) (rpc.Response, error) {
	return c.CallContext(context.Background(), envelope)
}

func (c *quicConn) CallContext(ctx context.Context, envelope []byte) (rpc.Response, error) {
	st, err := c.qc.OpenStreamSync(ctx)
	if err != nil {
		if c.IsClosed() {
			return rpc.Response{}, transport.ErrClosed
		}
		return rpc.Response{}, err
	}
	// Cancel the stream if ctx fires before we finish.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			st.CancelRead(0)
			st.CancelWrite(0)
		case <-c.closed:
			st.CancelRead(0)
			st.CancelWrite(0)
		case <-done:
		}
	}()

	if err := writeFrame(st, opCall, envelope); err != nil {
		return rpc.Response{}, c.mapErr(ctx, err)
	}
	_ = st.Close() // half-close send: server reads EOF after the request frame

	body, err := readFrame(st)
	if err != nil {
		return rpc.Response{}, c.mapErr(ctx, err)
	}
	return rpc.ParseResponse(body)
}

// OpenStream opens a fresh QUIC stream and returns a [transport.Stream] whose
// Send/Recv are length-prefixed writes/reads on that stream. The first frame
// carries the rpc open envelope (method + init); the peer's StreamHandler runs
// with it.
func (c *quicConn) OpenStream(method uint32, init []byte) (transport.Stream, error) {
	st, err := c.qc.OpenStreamSync(context.Background())
	if err != nil {
		if c.IsClosed() {
			return nil, transport.ErrClosed
		}
		return nil, err
	}
	env := rpc.BuildRequest(rpc.Call{Method: method, PromiseID: c.NextPromiseID(), Payload: init})
	if err := writeFrame(st, opStream, env); err != nil {
		st.CancelWrite(0)
		st.CancelRead(0)
		return nil, err
	}
	return newQUICStream(st, c.closed), nil
}

func (c *quicConn) NextPromiseID() uint32 { return c.promiseID.Add(1) }

func (c *quicConn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
	}
	// The QUIC conn may have died without a local Close.
	if ctx := c.qc.Context(); ctx != nil {
		select {
		case <-ctx.Done():
			c.markClosed()
			return true
		default:
		}
	}
	return false
}

func (c *quicConn) Close() error {
	c.markClosed()
	return c.qc.CloseWithError(0, "")
}

func (c *quicConn) markClosed() {
	c.closeOnce.Do(func() { close(c.closed) })
}

// TLS returns the negotiated TLS state of the QUIC connection (always present
// and always TLS 1.3 — QUIC mandates it — pinned to X25519MLKEM768 here).
func (c *quicConn) TLS() *tls.ConnectionState {
	cs := c.qc.ConnectionState().TLS
	return &cs
}

// mapErr turns a ctx/conn-close into the canonical transport errors.
func (c *quicConn) mapErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.IsClosed() {
		return transport.ErrClosed
	}
	return err
}

// quicStream realises [transport.Stream] over one *quic.Stream.
type quicStream struct {
	st       *qgo.Stream
	connDone chan struct{} // closed when the parent quicConn closes

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	sendDone bool
}

func newQUICStream(st *qgo.Stream, connDone chan struct{}) *quicStream {
	ctx, cancel := context.WithCancel(context.Background())
	s := &quicStream{st: st, connDone: connDone, ctx: ctx, cancel: cancel}
	// Cancel the stream context when the QUIC stream ends or the conn drops.
	go func() {
		select {
		case <-st.Context().Done():
		case <-connDone:
			st.CancelRead(0)
			st.CancelWrite(0)
		}
		cancel()
	}()
	return s
}

func (s *quicStream) Send(body []byte) error {
	if err := writeFrame(s.st, 0, body); err != nil {
		return s.mapErr(err)
	}
	return nil
}

func (s *quicStream) Recv() ([]byte, error) {
	body, err := readFrame(s.st)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, s.mapErr(err)
	}
	return body, nil
}

func (s *quicStream) CloseSend() error {
	s.mu.Lock()
	if s.sendDone {
		s.mu.Unlock()
		return nil
	}
	s.sendDone = true
	s.mu.Unlock()
	return s.st.Close() // half-close send: peer Recv sees io.EOF
}

func (s *quicStream) Context() context.Context { return s.ctx }

func (s *quicStream) mapErr(err error) error {
	select {
	case <-s.connDone:
		return transport.ErrClosed
	default:
	}
	return err
}

// writeFrame writes one length-prefixed frame. tag<=0 writes no tag byte
// (subsequent stream messages); a non-zero tag prefixes the opener byte.
func writeFrame(w io.Writer, tag byte, body []byte) error {
	var hdr [5]byte
	if tag != 0 {
		binary.LittleEndian.PutUint32(hdr[0:4], uint32(1+len(body)))
		hdr[4] = tag
		if _, err := w.Write(hdr[:5]); err != nil {
			return err
		}
	} else {
		binary.LittleEndian.PutUint32(hdr[0:4], uint32(len(body)))
		if _, err := w.Write(hdr[:4]); err != nil {
			return err
		}
	}
	_, err := w.Write(body)
	return err
}

// readFrame reads one untagged length-prefixed frame — a unary response or a
// stream message (the QUIC stream already scopes it, so no opener byte).
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n > maxFrame {
		return nil, errors.New("quic: frame length out of range")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// readFirstFrame reads the opener byte + length-prefixed body of a stream's
// first frame: [ uint32 len ][ 1 tag ][ body ] (len = 1 + len(body)).
func readFirstFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[0:4])
	if n < 1 || n > maxFrame {
		return 0, nil, errors.New("quic: frame length out of range")
	}
	body := make([]byte, n-1)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return hdr[4], body, nil
}

// Dial opens a PQ-secured QUIC connection to addr and returns a call-only
// [transport.Conn]. conf supplies the client's roots (or InsecureSkipVerify in
// tests); the PQ curve + ALPN are applied here. Each later Call/OpenStream
// opens its own QUIC stream.
func Dial(ctx context.Context, addr string, conf *tls.Config) (transport.Conn, error) {
	qc, err := qgo.DialAddr(ctx, addr, pqTLS(conf), nil)
	if err != nil {
		return nil, err
	}
	return newQUICConn(qc), nil
}

// Server accepts PQ-secured QUIC connections and serves unary dispatch and/or
// streaming on each accepted QUIC stream.
type Server struct {
	ln       *qgo.Listener
	dispatch transport.Dispatch
	stream   transport.StreamHandler // server-side stream dispatch (nil = unary only)
	ctx      context.Context
	cancel   context.CancelFunc
}

// Listen binds addr (UDP) and serves dispatch over PQ-secured QUIC. conf must
// carry a server certificate.
func Listen(addr string, conf *tls.Config, dispatch transport.Dispatch) (*Server, error) {
	return ListenStream(addr, conf, dispatch, nil)
}

// ListenStream binds addr (UDP) and serves BOTH unary requests (dispatch) and
// server-side streams (stream) over PQ-secured QUIC — the QUIC analogue of
// [transport.ListenStream]. conf must carry a server certificate.
func ListenStream(addr string, conf *tls.Config, dispatch transport.Dispatch, stream transport.StreamHandler) (*Server, error) {
	ln, err := qgo.ListenAddr(addr, pqTLS(conf), nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{ln: ln, dispatch: dispatch, stream: stream, ctx: ctx, cancel: cancel}
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

// serveConn accepts QUIC streams on one connection and dispatches each one on
// its own goroutine — unary or streaming per the opener byte.
func (s *Server) serveConn(qc *qgo.Conn) {
	for {
		st, err := qc.AcceptStream(s.ctx)
		if err != nil {
			return // conn closed
		}
		go s.serveStream(qc, st)
	}
}

func (s *Server) serveStream(qc *qgo.Conn, st *qgo.Stream) {
	tag, first, err := readFirstFrame(st)
	if err != nil {
		st.CancelRead(0)
		st.CancelWrite(0)
		return
	}
	switch tag {
	case opCall:
		if s.dispatch == nil {
			st.CancelWrite(0)
			return
		}
		resp, derr := s.dispatch(first)
		if derr != nil {
			if call, perr := rpc.ParseRequest(first); perr == nil {
				resp = rpc.BuildResponse(rpc.StatusInternal, call.PromiseID, nil)
			} else {
				st.CancelWrite(0)
				return
			}
		}
		_ = writeFrame(st, 0, resp)
		_ = st.Close()

	case opStream:
		if s.stream == nil {
			st.CancelWrite(0)
			return
		}
		call, perr := rpc.ParseRequest(first)
		if perr != nil {
			st.CancelWrite(0)
			return
		}
		connDone := make(chan struct{})
		go func() {
			<-qc.Context().Done()
			close(connDone)
		}()
		stream := newQUICStream(st, connDone)
		s.stream(call.Method, call.Payload, stream)
		_ = stream.CloseSend()

	default:
		st.CancelWrite(0)
	}
}

// Addr is the listener's UDP address (useful with ":0").
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Close stops accepting and closes the listener.
func (s *Server) Close() error {
	s.cancel()
	return s.ln.Close()
}
