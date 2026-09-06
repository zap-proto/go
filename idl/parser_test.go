// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package idl

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

func paramStruct(p *Param) string {
	if p == nil {
		return ""
	}
	return p.StructName
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

// A field named as its struct is refused, because the accessor a backend
// prints for it is a member named as the type is — which C++ reads as a
// constructor. Caught once, in the front end, rather than once per backend.
func TestAFieldMayNotRepeatItsStructsName(t *testing.T) {
	_, err := Parse("x.zap", []byte("package p\nstruct Hash {\n  Kind u8 @0\n  Hash bytes_fixed[32] @1\n}\n"))
	if err == nil {
		t.Fatal("a field named as its struct was accepted")
	}
	if !strings.Contains(err.Error(), "repeats the struct's name") {
		t.Fatalf("error does not say what is wrong: %v", err)
	}
}

// Two fields of one name would print one accessor twice.
func TestTwoFieldsOfOneNameAreRefused(t *testing.T) {
	_, err := Parse("x.zap", []byte("package p\nstruct S {\n  A u8 @0\n  A u8 @1\n}\n"))
	if err == nil {
		t.Fatal("a duplicate field name was accepted")
	}
	if !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("error does not say what is wrong: %v", err)
	}
}
