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
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/zap-proto/go/rpc"
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
