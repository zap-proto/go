// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read is the schema under a testdata name, parsed.
func read(t *testing.T, name string) *File {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(name, src)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRustGolden(t *testing.T) {
	for _, tc := range []struct{ schema, golden string }{
		{"basetx.zap", "basetx_zap.rs.golden"},
		{"echo.zap", "echo_zap.rs.golden"},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			files, err := EmitRust(read(t, tc.schema), defaultRustRuntime)
			if err != nil {
				t.Fatal(err)
			}
			name := strings.TrimSuffix(tc.schema, ".zap") + "_zap.rs"
			got, ok := files[name]
			if !ok {
				t.Fatalf("no %s among %v", name, keys(files))
			}
			path := filepath.Join("testdata", tc.golden)
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", path)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("emitted Rust differs from the golden.\n--- got ---\n%s", got)
			}
		})
	}
}

// TestRustCarriesItsRuntime — the emitted module calls a runtime, and the
// runtime travels with the generator. A schema with an interface also needs
// the call envelope.
func TestRustCarriesItsRuntime(t *testing.T) {
	plain, err := EmitRust(read(t, "basetx.zap"), defaultRustRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["zap.rs"]; !ok {
		t.Error("a schema's module was emitted without the runtime it calls")
	}
	if _, ok := plain["rpc.rs"]; ok {
		t.Error("a schema with no interface was given the call envelope")
	}
	service, err := EmitRust(read(t, "echo.zap"), defaultRustRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service["rpc.rs"]; !ok {
		t.Error("a schema with an interface was emitted without the call envelope")
	}
}

// TestOneFrontEnd — the whitespace-significant twin of a schema desugars to
// the same source, so it must emit the same Rust. If a backend ever grew its
// own parser this is the test that would catch it.
func TestOneFrontEnd(t *testing.T) {
	braces, err := EmitRust(read(t, "basetx.zap"), defaultRustRuntime)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("testdata", "ws", "basetx_ws.zap"))
	if err != nil {
		t.Fatal(err)
	}
	// The source name rides into the file header, so compare under one name.
	f, err := Parse("basetx.zap", src)
	if err != nil {
		t.Fatal(err)
	}
	space, err := EmitRust(f, defaultRustRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(braces["basetx_zap.rs"], space["basetx_zap.rs"]) {
		t.Error("the brace and the whitespace form of one schema emitted different Rust")
	}
}

// TestRustRuntimePath — a crate that files the runtime somewhere other than
// its root says so, and every use line follows.
func TestRustRuntimePath(t *testing.T) {
	files, err := EmitRust(read(t, "echo.zap"), "crate::wire")
	if err != nil {
		t.Fatal(err)
	}
	body := string(files["echo_zap.rs"])
	for _, want := range []string{"use crate::wire::rpc;", "use crate::wire::zap;"} {
		if !strings.Contains(body, want) {
			t.Errorf("emitted module does not say %q", want)
		}
	}
}

func TestSnake(t *testing.T) {
	for _, tc := range []struct{ in, snake, scream string }{
		{"NetworkID", "network_id", "NETWORK_ID"},
		{"BaseTx", "base_tx", "BASE_TX"},
		{"IDList", "id_list", "ID_LIST"},
		{"Memo", "memo", "MEMO"},
		{"ping", "ping", "PING"},
		{"A32", "a32", "A32"},
		{"TxID", "tx_id", "TX_ID"},
	} {
		if got := snakeCase2(tc.in); got != tc.snake {
			t.Errorf("snakeCase2(%q) = %q, want %q", tc.in, got, tc.snake)
		}
		if got := screamCase(tc.in); got != tc.scream {
			t.Errorf("screamCase(%q) = %q, want %q", tc.in, got, tc.scream)
		}
	}
}

// TestRustKeyword — a schema may name a field `type`; Rust may not. The
// emitted identifier takes the escape rather than the generator refusing a
// legal schema.
func TestRustKeyword(t *testing.T) {
	src := []byte("package k\n\nstruct T {\n    Type u32 @0\n    Move u32 @4\n    Self u32 @8\n}\n")
	f, err := Parse("k.zap", src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := EmitRust(f, defaultRustRuntime)
	if err != nil {
		t.Fatal(err)
	}
	body := string(files["k_zap.rs"])
	for _, want := range []string{"pub fn r#type(", "pub fn r#move(", "pub fn self_("} {
		if !strings.Contains(body, want) {
			t.Errorf("emitted module does not say %q", want)
		}
	}
}

// TestRustStable — two runs of the generator over one schema emit the same
// bytes. A map iterated in the wrong place would show up here.
func TestRustStable(t *testing.T) {
	for i := 0; i < 8; i++ {
		a, err := EmitRust(read(t, "echo.zap"), defaultRustRuntime)
		if err != nil {
			t.Fatal(err)
		}
		b, err := EmitRust(read(t, "echo.zap"), defaultRustRuntime)
		if err != nil {
			t.Fatal(err)
		}
		for name := range a {
			if !bytes.Equal(a[name], b[name]) {
				t.Fatalf("%s is not stable across runs", name)
			}
		}
	}
}

// TestRustRefusesNothing — an empty schema has nothing to emit, and says so
// rather than writing an empty module.
func TestRustRefusesNothing(t *testing.T) {
	if _, err := EmitRust(&File{Package: "empty"}, defaultRustRuntime); err == nil {
		t.Error("an empty schema emitted a module")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
