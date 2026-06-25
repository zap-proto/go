// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quic

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/zap-proto/go/rpc"
	"github.com/zap-proto/go/transport"
)

func genCert(t *testing.T) tls.Certificate {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "zap-quic-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("createcert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

func echoDispatch(envelope []byte) ([]byte, error) {
	call, err := rpc.ParseRequest(envelope)
	if err != nil {
		return nil, err
	}
	return rpc.BuildResponse(rpc.StatusOK, call.PromiseID, call.Payload), nil
}

// TestRoundTrip_QUIC_PQ proves an RPC round-trip over QUIC whose TLS 1.3
// handshake is pinned to X25519MLKEM768 on both ends — a successful
// handshake IS proof ZAP-over-QUIC ran with PQ X-Wing. Same transport.Conn
// contract as TCP/Unix; only the wire underneath changed.
func TestRoundTrip_QUIC_PQ(t *testing.T) {
	cert := genCert(t)
	srv, err := Listen("127.0.0.1:0",
		&tls.Config{Certificates: []tls.Certificate{cert}}, echoDispatch)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, srv.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req := rpc.BuildRequest(rpc.Call{
		Method:    1,
		PromiseID: conn.NextPromiseID(),
		Payload:   []byte("zap-over-quic-pq-x-wing"),
	})
	resp, err := conn.CallContext(ctx, req)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != rpc.StatusOK {
		t.Fatalf("status = %d, want OK", resp.Status)
	}
	if string(resp.Body) != "zap-over-quic-pq-x-wing" {
		t.Fatalf("body = %q", resp.Body)
	}
}

// TestConcurrentCalls_QUIC proves the native design: 64 Calls in flight at
// once, each on its OWN QUIC stream, every one getting back exactly its own
// distinct payload. With per-operation QUIC streams there is no head-of-line
// blocking across the concurrent calls.
func TestConcurrentCalls_QUIC(t *testing.T) {
	cert := genCert(t)
	srv, err := Listen("127.0.0.1:0",
		&tls.Config{Certificates: []tls.Certificate{cert}}, echoDispatch)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := Dial(ctx, srv.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("payload-%d", i)
			req := rpc.BuildRequest(rpc.Call{Method: 1, PromiseID: conn.NextPromiseID(), Payload: []byte(want)})
			resp, err := conn.CallContext(ctx, req)
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", i, err)
				return
			}
			if string(resp.Body) != want {
				errs <- fmt.Errorf("call %d: body = %q, want %q", i, resp.Body, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestRoundTrip_QUIC_Stream_PQ proves server-side streaming over PQ-secured QUIC
// via ListenStream — the streaming capability Listen (unary-only) lacked. A
// successful X25519MLKEM768 handshake plus the streamed frames arriving is proof
// ZAP streaming runs over PQ QUIC.
func TestRoundTrip_QUIC_Stream_PQ(t *testing.T) {
	const method = 9
	streamH := func(m uint32, init []byte, s transport.Stream) {
		if m != method {
			return
		}
		for _, f := range [][]byte{[]byte("1"), []byte("2"), []byte("3")} {
			if err := s.Send(append(append([]byte{}, init...), f...)); err != nil {
				return
			}
		}
		_ = s.CloseSend()
	}

	cert := genCert(t)
	srv, err := ListenStream("127.0.0.1:0",
		&tls.Config{Certificates: []tls.Certificate{cert}}, echoDispatch, streamH)
	if err != nil {
		t.Fatalf("ListenStream: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, srv.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	st, err := conn.OpenStream(method, []byte("q:"))
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	var got []string
	for {
		b, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, string(b))
	}
	if len(got) != 3 || got[0] != "q:1" || got[2] != "q:3" {
		t.Fatalf("streamed = %v, want [q:1 q:2 q:3]", got)
	}
}
