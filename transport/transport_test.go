// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zap-proto/go/rpc"
)

// echoDispatch echoes the request payload back as the response body. It is
// the network-side equivalent of the generated DispatchX(handler, env).
func echoDispatch(envelope []byte) ([]byte, error) {
	call, err := rpc.ParseRequest(envelope)
	if err != nil {
		return nil, err
	}
	return rpc.BuildResponse(rpc.StatusOK, call.PromiseID, call.Payload), nil
}

// shortSock returns a short Unix-socket path (macOS caps sun_path at 104
// bytes, so t.TempDir() under /var/folders is too long).
func shortSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "zt")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func roundTrip(t *testing.T, network, addr string) {
	t.Helper()
	conn, err := Dial(network, addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req := rpc.BuildRequest(rpc.Call{
		Method:    1,
		PromiseID: conn.NextPromiseID(),
		Payload:   []byte("hello-zap-no-protobuf"),
	})
	resp, err := conn.Call(req)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != rpc.StatusOK {
		t.Fatalf("status = %d, want OK", resp.Status)
	}
	if string(resp.Body) != "hello-zap-no-protobuf" {
		t.Fatalf("body = %q, want %q", resp.Body, "hello-zap-no-protobuf")
	}
}

// TestRoundTrip_TCP proves a request/response over a real TCP socket.
func TestRoundTrip_TCP(t *testing.T) {
	srv, err := Listen("tcp", "127.0.0.1:0", echoDispatch)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()
	roundTrip(t, "tcp", srv.Addr().String())
}

// TestRoundTrip_Unix proves the same over a Unix-domain socket — the
// transport mount and other local-IPC services need.
func TestRoundTrip_Unix(t *testing.T) {
	sock := shortSock(t)
	srv, err := Listen("unix", sock, echoDispatch)
	if err != nil {
		t.Fatalf("Listen(unix): %v", err)
	}
	defer srv.Close()
	roundTrip(t, "unix", sock)
}

// TestConcurrentCalls proves PromiseID correlation under load: 64 calls in
// flight at once, each must get back exactly its own distinct payload.
func TestConcurrentCalls(t *testing.T) {
	srv, err := Listen("tcp", "127.0.0.1:0", echoDispatch)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	conn, err := Dial("tcp", srv.Addr().String())
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
			req := rpc.BuildRequest(rpc.Call{
				Method:    1,
				PromiseID: conn.NextPromiseID(),
				Payload:   []byte(want),
			})
			resp, err := conn.Call(req)
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

// TestCallAfterClose proves an in-flight Call fails cleanly (not hangs) when
// the connection is torn down.
func TestCallAfterClose(t *testing.T) {
	srv, err := Listen("tcp", "127.0.0.1:0", echoDispatch)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()
	conn, err := Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()
	req := rpc.BuildRequest(rpc.Call{Method: 1, PromiseID: conn.NextPromiseID()})
	if _, err := conn.Call(req); err != ErrClosed {
		t.Fatalf("Call after Close = %v, want ErrClosed", err)
	}
}
