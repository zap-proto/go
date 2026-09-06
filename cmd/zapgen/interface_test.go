// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"strings"
	"testing"

	"github.com/zap-proto/go/idl"
)

// TestInterfaceWhitespaceEquivalence proves the whitespace (braceless)
// interface form parses to the same AST as the brace form: same methods,
// same ordinals, same param structs.
func TestInterfaceWhitespaceEquivalence(t *testing.T) {
	brace := `package demo
struct A { X u32 @0 }
struct B { Y u32 @0 }
interface Svc {
    foo(in: A) returns (out: B)
    bar(in: A)
    baz() returns (out: B)
    quux()
}
`
	ws := `package demo
struct A
    X u32
struct B
    Y u32
interface Svc
    foo(in: A) returns (out: B)
    bar(in: A)
    baz() returns (out: B)
    quux()
`
	bf, err := idl.Parse("brace.zap", []byte(brace))
	if err != nil {
		t.Fatalf("parse brace: %v", err)
	}
	wf, err := idl.Parse("ws.zap", []byte(ws))
	if err != nil {
		t.Fatalf("parse ws: %v", err)
	}
	if len(bf.Interfaces) != 1 || len(wf.Interfaces) != 1 {
		t.Fatalf("interface count: brace=%d ws=%d", len(bf.Interfaces), len(wf.Interfaces))
	}
	bm, wm := bf.Interfaces[0].Methods, wf.Interfaces[0].Methods
	if len(bm) != len(wm) {
		t.Fatalf("method count: brace=%d ws=%d", len(bm), len(wm))
	}
	for i := range bm {
		if bm[i].Name != wm[i].Name || bm[i].Ordinal != wm[i].Ordinal {
			t.Errorf("method %d: brace=(%s@%d) ws=(%s@%d)", i,
				bm[i].Name, bm[i].Ordinal, wm[i].Name, wm[i].Ordinal)
		}
		if idlParamStruct(bm[i].Request) != idlParamStruct(wm[i].Request) ||
			idlParamStruct(bm[i].Response) != idlParamStruct(wm[i].Response) {
			t.Errorf("method %s param mismatch brace vs ws", bm[i].Name)
		}
	}
	// And the generated code must be byte-identical across the two forms.
	bf.Source, wf.Source = "svc.zap", "svc.zap"
	bout, err := EmitSingle2(bf)
	if err != nil {
		t.Fatalf("emit brace: %v", err)
	}
	wout, err := EmitSingle2(wf)
	if err != nil {
		t.Fatalf("emit ws: %v", err)
	}
	if bout != wout {
		t.Errorf("brace vs ws generated code differs\n--- brace ---\n%s\n--- ws ---\n%s", bout, wout)
	}
}

// EmitSingle2 is a test shim returning the combined output as a string.
func EmitSingle2(f *idl.File) (string, error) {
	_, b, err := EmitSingle(f)
	return string(b), err
}

func TestInterfaceErrors(t *testing.T) {
	cases := map[string]string{
		"two request params":          "package p\nstruct A { X u8 @0 }\ninterface S {\n  f(a: A, b: A)\n}\n",
		"missing param type":          "package p\ninterface S {\n  f(a:)\n}\n",
		"unterminated iface":          "package p\ninterface S {\n  f()\n",
		"unknown param struct (emit)": "package p\ninterface S {\n  f(a: Nope)\n}\n",
	}
	for name, in := range cases {
		in := in
		t.Run(name, func(t *testing.T) {
			f, perr := idl.Parse("t.zap", []byte(in))
			if perr != nil {
				return // parse-time rejection is acceptable
			}
			// If it parsed, the emit-time validation must reject it.
			if _, _, eerr := EmitSingle(f); eerr == nil {
				t.Errorf("expected parse or emit error for %q", in)
			}
		})
	}
}

// TestInterfaceDuplicateMethodRejected pins that two methods with the same
// name are refused at emit (they would generate colliding Go identifiers).
func TestInterfaceDuplicateMethodRejected(t *testing.T) {
	src := "package p\ninterface S {\n  f()\n  f()\n}\n"
	f, err := idl.Parse("t.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, _, err = EmitSingle(f)
	if err == nil || !strings.Contains(err.Error(), "duplicate method") {
		t.Errorf("expected duplicate-method error, got %v", err)
	}
}

func idlParamStruct(p *idl.Param) string {
	if p == nil {
		return ""
	}
	return p.StructName
}
