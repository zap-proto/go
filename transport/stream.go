// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"context"
	"encoding/binary"
	"io"
	"sync"

	"github.com/zap-proto/go/rpc"
)

// StreamHandler serves one inbound stream on the server: method is the rpc
// ordinal from the open envelope, init its initial payload, and s the
// bidirectional [Stream] to Recv from / Send on. It runs on its own goroutine;
// returning half-closes the stream's send side (the peer's Recv sees io.EOF).
// This one handler covers server-, client-, and bidirectional streaming —
// the shape is just which side calls Send vs Recv.
type StreamHandler func(method uint32, init []byte, s Stream)

// muxStream is the byte-pipe realisation of [Stream], correlated by a streamID
// (the opener's PromiseID) and carried as dirStream* frames on its [muxConn].
type muxStream struct {
	conn *muxConn
	id   uint32

	// Inbound messages are QUEUED, not handed to a fixed-size channel that the
	// read loop blocks on. deliver() runs on muxConn.readLoop, and that loop
	// carries every frame on the connection — other streams' frames and every
	// unary response. If delivery can block, one slow stream consumer stalls
	// the whole connection: on a pooled conn (one per peer address, shared by
	// all callers) a single slow ListEntries froze every RPC in the process.
	// It also deadlocks outright when a stream consumer's own next call rides
	// the same connection, since the reply can never be read.
	//
	// So: append + wake, never block. maxStreamQueue bounds the memory a
	// runaway producer can pin, and overflowing FAILS THAT ONE STREAM instead
	// of the connection everyone shares.
	queue    [][]byte
	notify   chan struct{} // buffered(1) wakeup for a waiting Recv
	overflow bool

	// ctx is cancelled when the stream ends (half-close / handler return) OR
	// the connection drops (it derives from conn.ctx). A streaming handler
	// gates its idle wait on Context().Done() so a disconnected idle peer is
	// observed and the handler returns instead of leaking.
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	recvDone bool
	sendDone bool
}

// Context is cancelled when the stream ends or the connection drops. Server
// stream handlers should derive their work from it so a dropped peer (even an
// idle one with no in-flight frames) releases the handler.
func (s *muxStream) Context() context.Context { return s.ctx }

// OpenStream opens a client stream: allocates a streamID, sends the open
// frame carrying method + init, and returns the [Stream] to drive. The peer's
// [StreamHandler] is invoked with (method, init, its side of the stream).
func (c *muxConn) OpenStream(method uint32, init []byte) (Stream, error) {
	id := c.NextPromiseID()
	sctx, scancel := context.WithCancel(c.ctx)
	s := &muxStream{conn: c, id: id, notify: make(chan struct{}, 1), ctx: sctx, cancel: scancel}
	c.streamMu.Lock()
	c.streams[id] = s
	c.streamMu.Unlock()

	env := rpc.BuildRequest(rpc.Call{Method: method, PromiseID: id, Payload: init})
	if err := c.writeFrame(dirStreamOpen, env); err != nil {
		c.streamMu.Lock()
		delete(c.streams, id)
		c.streamMu.Unlock()
		return nil, err
	}
	return s, nil
}

// routeStream dispatches one inbound stream frame (body already copied off
// the read buffer).
func (c *muxConn) routeStream(dir byte, body []byte) {
	switch dir {
	case dirStreamOpen:
		call, err := rpc.ParseRequest(body)
		if err != nil {
			return
		}
		c.streamMu.Lock()
		h := c.streamHandler
		if h == nil {
			c.streamMu.Unlock()
			return // not a stream server
		}
		sctx, scancel := context.WithCancel(c.ctx)
		s := &muxStream{conn: c, id: call.PromiseID, notify: make(chan struct{}, 1), ctx: sctx, cancel: scancel}
		c.streams[call.PromiseID] = s
		c.streamMu.Unlock()
		init := append([]byte(nil), call.Payload...) // payload aliases body
		go func() {
			h(call.Method, init, s)
			s.cancel()        // handler returned -> release its Context
			_ = s.CloseSend() // half-close the send side
		}()

	case dirStreamMsg:
		if len(body) < 4 {
			return
		}
		id := binary.LittleEndian.Uint32(body[:4])
		c.streamMu.Lock()
		s := c.streams[id]
		c.streamMu.Unlock()
		if s != nil {
			s.deliver(body[4:])
		}

	case dirStreamEnd:
		if len(body) < 4 {
			return
		}
		id := binary.LittleEndian.Uint32(body[:4])
		c.streamMu.Lock()
		s := c.streams[id]
		c.streamMu.Unlock()
		if s != nil {
			s.closeRecv()
		}
	}
}

// Send writes one message on the stream's outbound half.
func (s *muxStream) Send(body []byte) error {
	frame := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(frame[:4], s.id)
	copy(frame[4:], body)
	return s.conn.writeFrame(dirStreamMsg, frame)
}

// CloseSend half-closes the outbound direction; the peer's [Stream.Recv]
// then returns io.EOF. Idempotent.
func (s *muxStream) CloseSend() error {
	s.mu.Lock()
	if s.sendDone {
		s.mu.Unlock()
		return nil
	}
	s.sendDone = true
	s.mu.Unlock()

	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], s.id)
	return s.conn.writeFrame(dirStreamEnd, hdr[:])
}

// Recv returns the next inbound message, io.EOF once the peer half-closes,
// or [ErrClosed] if the connection drops.
func (s *muxStream) Recv() ([]byte, error) {
	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			msg := s.queue[0]
			s.queue[0] = nil // release the reference as we advance
			s.queue = s.queue[1:]
			s.mu.Unlock()
			return msg, nil
		}
		over, done := s.overflow, s.recvDone
		s.mu.Unlock()

		// Drain fully before reporting the end, so a half-close or an overflow
		// never discards messages already queued ahead of it.
		if over {
			return nil, ErrStreamOverflow
		}
		if done {
			return nil, io.EOF
		}
		select {
		case <-s.notify:
		case <-s.conn.closed:
			return nil, ErrClosed
		}
	}
}

// maxStreamQueue bounds the messages one stream may hold undelivered. It is
// generous because the normal cause of a backlog is a consumer briefly busy,
// not a runaway peer; the point of the bound is only to keep a pathological
// producer from pinning memory without limit.
const maxStreamQueue = 8192

// deliver queues one inbound message. It NEVER blocks: it runs on the
// connection's read loop, which must keep draining for every other stream and
// every unary reply on that connection.
func (s *muxStream) deliver(msg []byte) {
	s.mu.Lock()
	if s.recvDone || s.overflow {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= maxStreamQueue {
		s.overflow = true
		s.mu.Unlock()
		s.cancel() // release a handler waiting on Context
		s.wake()
		return
	}
	s.queue = append(s.queue, msg)
	s.mu.Unlock()
	s.wake()
}

// wake nudges a blocked Recv. The channel is buffered(1) and coalescing: a
// pending wakeup already covers any number of queued messages.
func (s *muxStream) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *muxStream) closeRecv() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.recvDone {
		s.recvDone = true
		s.cancel() // peer half-closed -> release the handler's Context
		s.wake()   // a blocked Recv must observe the end and drain what remains
		s.conn.streamMu.Lock()
		delete(s.conn.streams, s.id)
		s.conn.streamMu.Unlock()
	}
}
