// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"
)

// Node is a minimal client for ZAP-native backends that speak the
// "one raw ZAP message per connection" wire protocol — the shape the Hanzo
// database forks implement in their native listeners (e.g. hanzo/sql's
// zap_fdw on :9651, hanzo/kv on :9653, hanzo/datastore on :9655,
// hanzo/documentdb on :9654). Each request is a whole ZAP message written
// to a fresh connection; the backend reads it, answers with one ZAP
// message, and closes the connection. Node frames the response by the ZAP
// header's own Size field, so it is robust to TCP segmentation.
//
// This is deliberately DISTINCT from the [github.com/zap-proto/go/transport]
// package. transport carries the rpc.Call/rpc.Response envelope over a
// length-prefixed, direction-tagged, symmetric, connection-reused frame
// mux — the contract for service-to-service RPC. Node carries a bare ZAP
// message with no extra framing, one message per connection — the contract
// the database backends expose. Two protocols, two packages, no braiding.
//
// Node is pure stdlib: it dials with [net] and never depends on mDNS or any
// external package. Auto-discovery of backends by service type is NOT part
// of this runtime; callers name the backend by explicit address via
// [Node.ConnectDirect]. (An mDNS-discovery layer, if ever wanted, is an
// optional build-tagged extra that would register peers through the same
// ConnectDirect seam — it is not a dependency of this file.)
//
// A Node is safe for concurrent use.
type Node struct {
	cfg  NodeConfig
	log  *slog.Logger
	dial func(ctx context.Context, addr string) (net.Conn, error)

	mu      sync.RWMutex
	peers   map[string]string // peerID -> dial address
	started bool
	stopped bool
}

// NodeConfig configures a client [Node].
type NodeConfig struct {
	// NodeID is this client's identity. Informational only — it labels log
	// lines and lets a backend correlate connections; it is not sent as part
	// of the DB wire messages.
	NodeID string

	// Port is retained for API compatibility with peer-to-peer Node
	// configurations. This client runtime does not bind an inbound listener
	// (serving inbound ZAP RPC is the transport package's role), so Port is
	// unused here; leave it 0.
	Port int

	// NoDiscovery disables mDNS auto-discovery. This runtime never discovers
	// (it is connect-by-explicit-address only), so the field is honored
	// trivially — set it true to document intent.
	NoDiscovery bool

	// Logger receives debug/error lines. Defaults to slog.Default().
	Logger *slog.Logger

	// DialContext optionally overrides how the Node establishes a connection
	// to a backend address. It defaults to a plaintext TCP dial, matching the
	// backends' native listeners. Inject a TLS/QUIC dialer here to reach a
	// PQ-mTLS-fronted backend without changing any call site.
	DialContext func(ctx context.Context, addr string) (net.Conn, error)

	// DialTimeout bounds a single connection attempt. Defaults to 10s.
	DialTimeout time.Duration
}

// ErrUnknownPeer is returned by [Node.Call] for a peerID that was never
// registered via [Node.ConnectDirect].
var ErrUnknownPeer = errors.New("zap: unknown peer (call ConnectDirect first)")

// ErrNodeStopped is returned once the node has been stopped.
var ErrNodeStopped = errors.New("zap: node stopped")

// maxNodeMessage bounds a single response message so a corrupt or hostile
// Size field cannot drive an unbounded allocation. 64 MiB is far above any
// real ORM request/response.
const maxNodeMessage = 64 << 20

// NewNode creates a client Node. It does not touch the network until
// [Node.Start] and [Node.ConnectDirect].
func NewNode(cfg NodeConfig) *Node {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	dial := cfg.DialContext
	if dial == nil {
		d := &net.Dialer{}
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp", addr)
		}
	}
	return &Node{
		cfg:   cfg,
		log:   log,
		dial:  dial,
		peers: make(map[string]string),
	}
}

// Start readies the node. For this client runtime it binds no listener and
// starts no discovery; it only marks the node usable and rejects a restart
// after Stop. It exists so callers written against a peer-to-peer Node API
// (Start/Stop lifecycle) work unchanged.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return ErrNodeStopped
	}
	n.started = true
	return nil
}

// Stop releases the node. In-flight Calls each own their connection and are
// unaffected by a later Stop; new Calls after Stop fail with ErrNodeStopped.
func (n *Node) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stopped = true
	n.peers = make(map[string]string)
}

// ConnectDirect registers a backend at addr ("host:port") as a peer and
// verifies it is reachable by dialing once (a connectable backend is the
// precondition every later Call depends on, so failing fast here surfaces a
// bad address at open time rather than on the first query). The peer's ID is
// its address; [Node.Peers] returns it and [Node.Call] takes it.
func (n *Node) ConnectDirect(addr string) error {
	if addr == "" {
		return errors.New("zap: ConnectDirect requires a non-empty address")
	}
	n.mu.RLock()
	stopped := n.stopped
	n.mu.RUnlock()
	if stopped {
		return ErrNodeStopped
	}

	ctx, cancel := context.WithTimeout(context.Background(), n.cfg.DialTimeout)
	defer cancel()
	conn, err := n.dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("zap: connect %s: %w", addr, err)
	}
	_ = conn.Close()

	n.mu.Lock()
	n.peers[addr] = addr
	n.mu.Unlock()
	n.log.Debug("zap: connected", "node", n.cfg.NodeID, "peer", addr)
	return nil
}

// Peers returns the registered peer IDs, sorted for determinism.
func (n *Node) Peers() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	ids := make([]string, 0, len(n.peers))
	for id := range n.peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Call ships one ZAP message to the named peer and returns its one-message
// answer. It opens a fresh connection (the backends answer once and close),
// writes the whole request, reads the response framed by the ZAP header
// Size field, and parses it. ctx bounds the whole exchange: its deadline is
// applied to the socket and its cancellation tears the connection down.
func (n *Node) Call(ctx context.Context, peerID string, msg *Message) (*Message, error) {
	if msg == nil {
		return nil, errors.New("zap: Call requires a non-nil message")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	n.mu.RLock()
	stopped := n.stopped
	addr, ok := n.peers[peerID]
	n.mu.RUnlock()
	if stopped {
		return nil, ErrNodeStopped
	}
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownPeer, peerID)
	}

	dialCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, n.cfg.DialTimeout)
		defer cancel()
	}
	conn, err := n.dial(dialCtx, addr)
	if err != nil {
		return nil, fmt.Errorf("zap: dial %s: %w", addr, err)
	}
	defer conn.Close()

	// Apply ctx deadline to the socket and tear the connection down on
	// cancellation so a blocked Write/Read returns instead of hanging.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()

	if _, err := conn.Write(msg.Bytes()); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("zap: write to %s: %w", addr, err)
	}

	resp, err := readFramedMessage(conn)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("zap: read from %s: %w", addr, err)
	}
	return resp, nil
}

// readFramedMessage reads exactly one ZAP message from r, using the header's
// Size field to know how many bytes to read. This is the framing the DB
// backends use: no length prefix, the message is self-delimiting via its
// own 16-byte header (Magic + Version + Flags + RootOffset + Size).
func readFramedMessage(r io.Reader) (*Message, error) {
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != Magic {
		return nil, ErrInvalidMagic
	}
	size := int(binary.LittleEndian.Uint32(hdr[12:16]))
	if size < HeaderSize {
		return nil, ErrBufferTooSmall
	}
	if size > maxNodeMessage {
		return nil, fmt.Errorf("zap: response size %d exceeds max %d", size, maxNodeMessage)
	}
	buf := make([]byte, size)
	copy(buf, hdr[:])
	if _, err := io.ReadFull(r, buf[HeaderSize:]); err != nil {
		return nil, err
	}
	return Parse(buf)
}
