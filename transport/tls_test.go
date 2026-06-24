// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
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
