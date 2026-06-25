// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package transport is the network plumbing for ZAP RPC: it carries the
// [github.com/zap-proto/go/rpc] Call/Response envelopes over a real
// connection (TCP, Unix socket, or QUIC) and correlates each response to
// its request by PromiseID.
//
// It is deliberately thin. The codegen already emits typed clients against
// a one-method channel interface —
//
//	type <Service>Channel interface {
//	    Call(envelope []byte) (rpc.Response, error)
//	}
//
// — and a server-side dispatch function, Dispatch<Service>(handler,
// envelope) ([]byte, error). In tests these are wired in-memory (the
// generated loopback channel calls DispatchX directly). This package is the
// drop-in NETWORK realisation of the same two contracts: [Conn] implements
// the client Channel, and [Listen] serves a [Dispatch] (a DispatchX bound to
// a handler) over accepted connections. No generated code changes — the
// only difference is that the bytes now cross a socket.
//
// Framing is length-prefixed and direction-tagged so one connection is
// fully symmetric (either end may both call and serve — unary today,
// bidirectional streaming when the stream layer lands):
//
//	[ uint32 len ][ 1 byte dir ][ rpc envelope bytes ]   (len = 1 + len(envelope))
//
// Correlation lives in the envelope itself (rpc.Call.PromiseID /
// rpc.Response.PromiseID), so the transport adds no correlation header of
// its own — it reads the PromiseID out of the envelope it is already
// shipping. Pure Go; no CGO. TCP and Unix are built in here; QUIC + the
// PQ X-Wing (X25519MLKEM768) handshake live in quic.go behind the same
// [Conn]/[Dispatch] contract.
package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/zap-proto/go/rpc"
)

// Frame direction tags. A request carries a Call envelope and expects a
// correlated response; a response carries a Response envelope answering a
// prior request's PromiseID.
const (
	dirRequest  byte = 1
	dirResponse byte = 2
	// Streaming frames. A stream is identified by the opener's PromiseID
	// (streamID). Open carries an rpc request envelope (method + initial
	// payload, PromiseID = streamID); Msg/End carry a 4-byte streamID
	// prefix then the body. Either end may send Msg until it sends End
	// (half-close), so the same three frames serve server-, client-, and
	// bidirectional streaming.
	dirStreamOpen byte = 3
	dirStreamMsg  byte = 4
	dirStreamEnd  byte = 5
)

// maxFrame bounds a single envelope so a hostile or corrupt length prefix
// cannot drive an unbounded allocation. 64 MiB is far above any real RPC
// message (the largest S3 messages are volume bulk frames, themselves
// chunk-bounded well under this).
const maxFrame = 64 << 20

// maxInFlight bounds the inbound requests dispatched concurrently on one
// connection. Without it, a peer pipelining N request frames spawns N
// goroutines (and N handler runs) at once — a single-socket
// resource-exhaustion amplifier. At the cap the read loop blocks (TCP
// backpressure) until a slot frees, so memory and goroutines stay bounded.
const maxInFlight = 256

// ErrClosed is returned by [Conn.Call] when the connection is shut down
// (locally via [Conn.Close] or by the peer) before the response arrives.
var ErrClosed = errors.New("transport: connection closed")

// Dispatch turns a request envelope into a response envelope. It matches
// the signature of the codegen-emitted DispatchX functions bound to a
// handler — e.g. `func(env []byte) ([]byte, error) { return DispatchEcho(h,
// env) }`. A nil Dispatch makes a connection call-only (no inbound serving).
type Dispatch func(envelope []byte) ([]byte, error)

// Channel is the client contract the generated typed clients consume (it is
// structurally identical to each generated <Service>Channel). [Conn]
// satisfies it.
type Channel interface {
	Call(envelope []byte) (rpc.Response, error)
}

// Conn is one ZAP-RPC connection. It is symmetric: every Conn runs a read
// loop that delivers inbound responses to waiting [Conn.Call]s and, when a
// Dispatch was supplied, serves inbound requests. Safe for concurrent use.
type Conn struct {
	nc       io.ReadWriteCloser // *net.TCPConn / *tls.Conn / quic stream
	dispatch Dispatch

	wmu sync.Mutex // serialises frame writes (one writer at a time)

	promiseID atomic.Uint32 // local PromiseID counter for outbound calls

	pendMu  sync.Mutex
	pending map[uint32]chan rpc.Response

	streamMu      sync.Mutex
	streams       map[uint32]*Stream // active streams by streamID
	streamHandler StreamHandler      // server-side stream dispatch (nil = none)

	sem chan struct{} // bounds concurrent inbound dispatches (backpressure)

	closeOnce sync.Once
	closed    chan struct{}

	// ctx is cancelled exactly when the connection closes; every server-side
	// stream derives its Context from it, so a handler that blocks on
	// Stream.Context().Done() (e.g. a long-lived subscription idle when the
	// peer disconnects) is released instead of leaking.
	ctx    context.Context
	cancel context.CancelFunc
}

// NewConn wraps an established stream (a *net.TCPConn / Unix conn, a
// *tls.Conn, or a QUIC stream — anything io.ReadWriteCloser) and starts its
// read loop. dispatch may be nil for a call-only endpoint. The returned Conn
// owns nc and closes it on [Conn.Close] or peer EOF.
func NewConn(nc io.ReadWriteCloser, dispatch Dispatch) *Conn {
	return newConn(nc, dispatch, nil)
}

// newConn is the full constructor; the streamHandler is set BEFORE the read
// loop starts so an immediate inbound stream-open is never dropped.
func newConn(nc io.ReadWriteCloser, dispatch Dispatch, sh StreamHandler) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		nc:            nc,
		dispatch:      dispatch,
		streamHandler: sh,
		pending:       make(map[uint32]chan rpc.Response),
		streams:       make(map[uint32]*Stream),
		sem:           make(chan struct{}, maxInFlight),
		closed:        make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}
	go c.readLoop()
	return c
}

// Dial connects to addr over network ("tcp" or "unix") and returns a
// call-only [Conn]. Use [DialServe] to also serve inbound requests on the
// same connection (bidirectional peers).
func Dial(network, addr string) (*Conn, error) {
	return DialServe(network, addr, nil)
}

// DialServe is [Dial] plus an inbound Dispatch, for a peer that both calls
// and serves over one connection.
func DialServe(network, addr string, dispatch Dispatch) (*Conn, error) {
	nc, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return NewConn(nc, dispatch), nil
}

// Call ships a request envelope and blocks until the correlated response
// arrives or the connection closes. It implements [Channel], so a *Conn is
// the network channel a generated client dials over:
//
//	conn, _ := transport.Dial("unix", sock)
//	client := mountwire.NewHanzoMountClient(conn, nil)
//
// The PromiseID is read from the envelope (the generated client already
// stamped it from its rpc.Session), so request and response correlate
// without a transport-level id.
func (c *Conn) Call(envelope []byte) (rpc.Response, error) {
	call, err := rpc.ParseRequest(envelope)
	if err != nil {
		return rpc.Response{}, err
	}
	ch := make(chan rpc.Response, 1)
	c.pendMu.Lock()
	c.pending[call.PromiseID] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		delete(c.pending, call.PromiseID)
		c.pendMu.Unlock()
	}()

	if err := c.writeFrame(dirRequest, envelope); err != nil {
		return rpc.Response{}, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-c.closed:
		return rpc.Response{}, ErrClosed
	}
}

// CallContext is [Conn.Call] that also aborts when ctx is done.
func (c *Conn) CallContext(ctx context.Context, envelope []byte) (rpc.Response, error) {
	call, err := rpc.ParseRequest(envelope)
	if err != nil {
		return rpc.Response{}, err
	}
	ch := make(chan rpc.Response, 1)
	c.pendMu.Lock()
	c.pending[call.PromiseID] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		delete(c.pending, call.PromiseID)
		c.pendMu.Unlock()
	}()

	if err := c.writeFrame(dirRequest, envelope); err != nil {
		return rpc.Response{}, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return rpc.Response{}, ctx.Err()
	case <-c.closed:
		return rpc.Response{}, ErrClosed
	}
}

// NextPromiseID hands out a monotonic local PromiseID. Generated clients
// carry their own rpc.Session, so this is only needed by hand-written
// callers that build envelopes directly.
func (c *Conn) NextPromiseID() uint32 { return c.promiseID.Add(1) }

// IsClosed reports whether the connection has been torn down — either
// locally via [Conn.Close] or by the peer / a read error closing the read
// loop. A connection-pool keeper uses it to evict and redial dead entries.
func (c *Conn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// Close shuts the connection and fails every in-flight Call with ErrClosed.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.cancel() // release every stream handler blocked on its Context
		_ = c.nc.Close()
	})
	return nil
}

func (c *Conn) readLoop() {
	defer c.Close()
	for {
		dir, body, err := readFrame(c.nc)
		if err != nil {
			return // EOF or read error tears the connection down
		}
		switch dir {
		case dirResponse:
			resp, perr := rpc.ParseResponse(body)
			if perr != nil {
				continue // skip a malformed response frame
			}
			c.pendMu.Lock()
			ch := c.pending[resp.PromiseID]
			c.pendMu.Unlock()
			if ch != nil {
				ch <- resp // buffered(1); the waiter always drains exactly one
			}
		case dirRequest:
			if c.dispatch == nil {
				continue // call-only endpoint: ignore inbound requests
			}
			// Copy the envelope: readFrame's buffer is reused next iteration,
			// and dispatch runs on its own goroutine.
			env := make([]byte, len(body))
			copy(env, body)
			// Acquire an in-flight slot before spawning. When maxInFlight are
			// already running the read loop blocks here, applying backpressure
			// to the peer instead of spawning unbounded goroutines. serve
			// releases the slot.
			select {
			case c.sem <- struct{}{}:
				go c.serve(env)
			case <-c.closed:
				return
			}
		case dirStreamOpen, dirStreamMsg, dirStreamEnd:
			b := make([]byte, len(body)) // hand a stable copy to the stream
			copy(b, body)
			c.routeStream(dir, b)
		}
	}
}

// serve dispatches one inbound request and writes its response. A dispatch
// (protocol) error still yields a StatusInternal response so the caller's
// Call never hangs.
func (c *Conn) serve(envelope []byte) {
	defer func() { <-c.sem }() // release the in-flight slot acquired by readLoop
	respBytes, err := c.dispatch(envelope)
	if err != nil {
		// Best-effort: answer the PromiseID with StatusInternal so the peer
		// unblocks rather than waiting out its context.
		if call, perr := rpc.ParseRequest(envelope); perr == nil {
			respBytes = rpc.BuildResponse(rpc.StatusInternal, call.PromiseID, nil)
		} else {
			return
		}
	}
	_ = c.writeFrame(dirResponse, respBytes)
}

// writeFrame writes one direction-tagged, length-prefixed frame. Writes are
// serialised so concurrent Calls and inbound responses never interleave on
// the wire.
func (c *Conn) writeFrame(dir byte, envelope []byte) error {
	var hdr [5]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(1+len(envelope)))
	hdr[4] = dir

	c.wmu.Lock()
	defer c.wmu.Unlock()
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	if tc, ok := c.nc.(*net.TCPConn); ok {
		// Single writev(2): header + body, no body copy.
		bufs := net.Buffers{hdr[:], envelope}
		_, err := bufs.WriteTo(tc)
		return err
	}
	if _, err := c.nc.Write(hdr[:]); err != nil {
		return err
	}
	_, err := c.nc.Write(envelope)
	return err
}

// readFrame reads one frame's direction byte and envelope. The returned
// slice is valid only until the next readFrame on the same connection.
func readFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[0:4])
	if n < 1 || n > maxFrame {
		return 0, nil, errors.New("transport: frame length out of range")
	}
	body := make([]byte, n-1) // n counts the 1 direction byte
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return hdr[4], body, nil
}
