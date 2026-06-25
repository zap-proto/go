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
// bidirectional stream to Recv from / Send on. It runs on its own goroutine;
// returning half-closes the stream's send side (the peer's Recv sees io.EOF).
// This one handler covers server-, client-, and bidirectional streaming —
// the shape is just which side calls Send vs Recv.
type StreamHandler func(method uint32, init []byte, s *Stream)

// Stream is a bidirectional sequence of message frames correlated by a
// streamID (the opener's PromiseID). Recv yields inbound messages until the
// peer half-closes (io.EOF); Send/CloseSend drive the outbound half. Safe
// for one concurrent Send and one concurrent Recv — the standard streaming
// RPC usage.
type Stream struct {
	conn *Conn
	id   uint32
	recv chan []byte

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
func (s *Stream) Context() context.Context { return s.ctx }

// OpenStream opens a client stream: allocates a streamID, sends the open
// frame carrying method + init, and returns the Stream to drive. The peer's
// [StreamHandler] is invoked with (method, init, its side of the stream).
func (c *Conn) OpenStream(method uint32, init []byte) (*Stream, error) {
	id := c.NextPromiseID()
	sctx, scancel := context.WithCancel(c.ctx)
	s := &Stream{conn: c, id: id, recv: make(chan []byte, 16), ctx: sctx, cancel: scancel}
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
func (c *Conn) routeStream(dir byte, body []byte) {
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
		s := &Stream{conn: c, id: call.PromiseID, recv: make(chan []byte, 16), ctx: sctx, cancel: scancel}
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
			select {
			case s.recv <- body[4:]:
			case <-c.closed:
			}
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
func (s *Stream) Send(body []byte) error {
	frame := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(frame[:4], s.id)
	copy(frame[4:], body)
	return s.conn.writeFrame(dirStreamMsg, frame)
}

// CloseSend half-closes the outbound direction; the peer's [Stream.Recv]
// then returns io.EOF. Idempotent.
func (s *Stream) CloseSend() error {
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
func (s *Stream) Recv() ([]byte, error) {
	select {
	case msg, ok := <-s.recv:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case <-s.conn.closed:
		return nil, ErrClosed
	}
}

func (s *Stream) closeRecv() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.recvDone {
		s.recvDone = true
		s.cancel() // peer half-closed -> release the handler's Context
		close(s.recv)
		s.conn.streamMu.Lock()
		delete(s.conn.streams, s.id)
		s.conn.streamMu.Unlock()
	}
}
