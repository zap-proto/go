// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"strings"
	"testing"
)

// TestParseInterfaceBrace parses the brace form of an interface and checks
// the method ordinals (1-based, declaration order) and param shapes.
func TestParseInterfaceBrace(t *testing.T) {
	src := `package demo
struct A { X u32 @0 }
struct B { Y u32 @0 }
interface Svc {
    foo(in: A) returns (out: B)
    bar(in: A)
    baz() returns (out: B)
    quux()
}
`
	f, err := Parse("svc.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Interfaces) != 1 {
		t.Fatalf("want 1 interface, got %d", len(f.Interfaces))
	}
	iface := f.Interfaces[0]
	if iface.Name != "Svc" {
		t.Errorf("name = %q, want Svc", iface.Name)
	}
	want := []struct {
		name           string
		ord            int
		req, resp      bool
		reqStr, respSt string
	}{
		{"foo", 1, true, true, "A", "B"},
		{"bar", 2, true, false, "A", ""},
		{"baz", 3, false, true, "", "B"},
		{"quux", 4, false, false, "", ""},
	}
	if len(iface.Methods) != len(want) {
		t.Fatalf("want %d methods, got %d", len(want), len(iface.Methods))
	}
	for i, w := range want {
		m := iface.Methods[i]
		if m.Name != w.name || m.Ordinal != w.ord {
			t.Errorf("method %d = (%s @%d), want (%s @%d)", i, m.Name, m.Ordinal, w.name, w.ord)
		}
		if (m.Request != nil) != w.req || (m.Response != nil) != w.resp {
			t.Errorf("method %s req/resp presence = %v/%v, want %v/%v",
				m.Name, m.Request != nil, m.Response != nil, w.req, w.resp)
		}
		if w.req && m.Request.StructName != w.reqStr {
			t.Errorf("method %s req struct = %q, want %q", m.Name, m.Request.StructName, w.reqStr)
		}
		if w.resp && m.Response.StructName != w.respSt {
			t.Errorf("method %s resp struct = %q, want %q", m.Name, m.Response.StructName, w.respSt)
		}
	}
}

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
	bf, err := Parse("brace.zap", []byte(brace))
	if err != nil {
		t.Fatalf("parse brace: %v", err)
	}
	wf, err := Parse("ws.zap", []byte(ws))
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
		if paramStruct(bm[i].Request) != paramStruct(wm[i].Request) ||
			paramStruct(bm[i].Response) != paramStruct(wm[i].Response) {
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

func paramStruct(p *Param) string {
	if p == nil {
		return ""
	}
	return p.StructName
}

// EmitSingle2 is a test shim returning the combined output as a string.
func EmitSingle2(f *File) (string, error) {
	_, b, err := EmitSingle(f)
	return string(b), err
}

// TestInterfaceWhitespaceBlockOpener proves `interface X` opens a
// whitespace block (methods indented under it) and that the brace is
// synthesized, while a struct field merely NAMED `interface` is still a
// field (the keyword+one-identifier+EOL rule).
func TestInterfaceWhitespaceBlockOpener(t *testing.T) {
	// `interface Svc` then indented methods -> interface with 1 method.
	src := "package p\ninterface Svc\n    ping()\n"
	f, err := Parse("p.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Interfaces) != 1 || len(f.Interfaces[0].Methods) != 1 {
		t.Fatalf("want 1 interface w/ 1 method, got %+v", f.Interfaces)
	}

	// `interface text` as a struct field (has a type) stays a field.
	src2 := "package p\nstruct S\n    interface text\n    B u8\n"
	f2, err := Parse("p2.zap", []byte(src2))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f2.Interfaces) != 0 {
		t.Fatalf("`interface text` field must not parse as a service: %+v", f2.Interfaces)
	}
	if len(f2.Structs) != 1 || len(f2.Structs[0].Fields) != 2 {
		t.Fatalf("want struct S with 2 fields, got %+v", f2.Structs)
	}
	if f2.Structs[0].Fields[0].Name != "interface" {
		t.Errorf("field 0 name = %q, want interface", f2.Structs[0].Fields[0].Name)
	}
}

// TestInterfaceErrors covers malformed interface input.
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
			f, perr := Parse("t.zap", []byte(in))
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
	f, err := Parse("t.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, _, err = EmitSingle(f)
	if err == nil || !strings.Contains(err.Error(), "duplicate method") {
		t.Errorf("expected duplicate-method error, got %v", err)
	}
}
