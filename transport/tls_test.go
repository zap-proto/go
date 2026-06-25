// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/zap-proto/go/rpc"
)

// genCert makes an ephemeral self-signed ed25519 certificate for localhost.
func genCert(t *testing.T) tls.Certificate {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "zap-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:              []string{"localhost"},
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

// TestRoundTrip_PQTLS proves an RPC round-trip over a TLS connection that is
// restricted to the X25519MLKEM768 hybrid PQ key exchange on BOTH ends — so
// a successful handshake IS proof the PQ X-Wing exchange was used.
func TestRoundTrip_PQTLS(t *testing.T) {
	cert := genCert(t)
	srv, err := ListenTLS("tcp", "127.0.0.1:0",
		PQTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}), echoDispatch)
	if err != nil {
		t.Fatalf("ListenTLS: %v", err)
	}
	defer srv.Close()

	conn, err := DialTLS("tcp", srv.Addr().String(),
		PQTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	defer conn.Close()

	req := rpc.BuildRequest(rpc.Call{
		Method:    1,
		PromiseID: conn.NextPromiseID(),
		Payload:   []byte("pq-x-wing"),
	})
	resp, err := conn.Call(req)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != rpc.StatusOK || string(resp.Body) != "pq-x-wing" {
		t.Fatalf("resp = (%d, %q)", resp.Status, resp.Body)
	}

	// Confirm TLS 1.3 was negotiated (X25519MLKEM768 mandates 1.3).
	st := conn.TLS()
	if st == nil {
		t.Fatal("TLS() = nil over a TLS connection")
	}
	if st.Version != tls.VersionTLS13 {
		t.Fatalf("TLS version = 0x%04x, want TLS1.3", st.Version)
	}
}

// TestPQRequired_NoClassicalDowngrade proves the PQ requirement is enforced:
// a PQ-only server REFUSES a classical-only (X25519) client rather than
// downgrading. This is the guarantee that "PQ connection" means PQ, not
// best-effort.
func TestPQRequired_NoClassicalDowngrade(t *testing.T) {
	cert := genCert(t)
	srv, err := ListenTLS("tcp", "127.0.0.1:0",
		PQTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}), echoDispatch)
	if err != nil {
		t.Fatalf("ListenTLS: %v", err)
	}
	defer srv.Close()

	classicalOnly := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		CurvePreferences:   []tls.CurveID{tls.X25519}, // no PQ on offer
	}
	if _, err := DialTLS("tcp", srv.Addr().String(), classicalOnly); err == nil {
		t.Fatal("PQ-only server accepted a classical-only client; downgrade not prevented")
	}
}

// TestRoundTrip_PQTLS_Unix proves PQ TLS works over a Unix socket too (local
// IPC with a post-quantum channel).
func TestRoundTrip_PQTLS_Unix(t *testing.T) {
	cert := genCert(t)
	sock := shortSock(t)
	srv, err := ListenTLS("unix", sock,
		PQTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}), echoDispatch)
	if err != nil {
		t.Fatalf("ListenTLS(unix): %v", err)
	}
	defer srv.Close()

	conn, err := DialTLS("unix", sock, PQTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	if err != nil {
		t.Fatalf("DialTLS(unix): %v", err)
	}
	defer conn.Close()

	req := rpc.BuildRequest(rpc.Call{Method: 1, PromiseID: conn.NextPromiseID(), Payload: []byte("x")})
	if resp, err := conn.Call(req); err != nil || resp.Status != rpc.StatusOK {
		t.Fatalf("Call over PQ-unix = (%v, %v)", resp.Status, err)
	}
}

// TestRoundTrip_PQTLS_Stream proves SERVER-SIDE STREAMING works over PQ-secured
// TLS via ListenStreamTLS — the capability ListenTLS (unary-only) lacks. The
// server streams N frames on an inbound stream; the DialTLS client OpenStreams
// and drains them to io.EOF. A successful X25519MLKEM768 handshake + the frames
// arriving is proof streaming-over-PQ-TLS round-trips.
func TestRoundTrip_PQTLS_Stream(t *testing.T) {
	const method = 7
	frames := [][]byte{[]byte("a"), []byte("b"), []byte("c")}

	streamH := func(m uint32, init []byte, s Stream) {
		if m != method {
			return
		}
		for _, f := range frames {
			if err := s.Send(append(append([]byte{}, init...), f...)); err != nil {
				return
			}
		}
		_ = s.CloseSend()
	}

	cert := genCert(t)
	srv, err := ListenStreamTLS("tcp", "127.0.0.1:0",
		PQTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}), echoDispatch, streamH)
	if err != nil {
		t.Fatalf("ListenStreamTLS: %v", err)
	}
	defer srv.Close()

	conn, err := DialTLS("tcp", srv.Addr().String(),
		PQTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	defer conn.Close()

	st, err := conn.OpenStream(method, []byte("x:"))
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
	if len(got) != 3 || got[0] != "x:a" || got[2] != "x:c" {
		t.Fatalf("streamed frames = %v, want [x:a x:b x:c]", got)
	}
	if ts := conn.TLS(); ts == nil || ts.Version != tls.VersionTLS13 {
		t.Fatal("not TLS1.3 / not a TLS conn — PQ X-Wing requires TLS1.3")
	}
}
