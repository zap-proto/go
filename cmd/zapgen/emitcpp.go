// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

// The C++ backend.
//
// One front end, two back ends. This file shares the parser, the desugar, the
// schema model and the validation with the Go backend in emit.go, and differs
// only in what it prints. A schema that emits Go here emits C++ from the same
// AST, the same offsets and the same ordinals, so the two languages cannot
// drift apart without the shared model drifting first.
//
// What a struct becomes:
//
//	offsets  — one constexpr per field, plus the fixed-section size
//	reader   — a class holding a zap::Object: every accessor loads from the
//	           borrowed buffer at its offset. No owned copy of any field, no
//	           parse pass, nothing allocated.
//	builder  — an Input record of plain field values and a New function that
//	           writes one buffer and returns it.
//
// What an interface becomes: the ordinal table, the channel the client ships
// over, the typed client, the handler contract, and the dispatch that routes
// an envelope by ordinal — the same four pieces the Go backend emits, over the
// same envelope.
//
// The emitted code calls the C++ ZAP runtime (github.com/zap-proto/cpp,
// <zap/zap.hpp> and <zap/rpc.hpp>) exactly as the emitted Go calls the Go one.
// No byte order, no header layout and no pointer arithmetic is spelled here:
// offsets come from the schema and every load and store goes through the
// runtime. That is the whole reason this backend exists — the wire lives in
// one place per language, and the schema says where the fields are.

import (
	"bytes"
	"fmt"
)

// cppBuilderVersion is the wire version the generated C++ builder writes.
//
// It is spelled rather than defaulted because the two runtimes default
// differently: zap.NewBuilder writes version 1, zap::Builder writes version 2.
// A generated builder that took either default would emit a different header
// in each language from one schema, and the first thing this backend has to be
// is byte-for-byte with the Go one. The data segment is identical across
// versions; only header byte 4 differs.
const cppBuilderVersion = "zap::kVersion1"

// EmitCPP emits one header per struct AND one per interface in f, keyed by
// basename. Mirrors Emit's contract so a caller can pick a backend without
// changing anything else.
func EmitCPP(f *File) (map[string][]byte, error) {
	out := make(map[string][]byte, len(f.Structs)+len(f.Interfaces))
	for _, s := range f.Structs {
		if err := validate(s); err != nil {
			return nil, err
		}
		var w bytes.Buffer
		one := []*Struct{s}
		writeCPPPrologue(&w, f, cppNeeds(one, nil))
		writeCPPIncludes(&w, f, s)
		writeCPPOpen(&w, f)
		writeCPPForwards(&w, one)
		emitCPPStruct(&w, s)
		emitCPPOutOfLine(&w, one)
		writeCPPClose(&w, f)
		out[snakeCase(s.Name)+"_zap.hpp"] = w.Bytes()
	}
	for _, iface := range f.Interfaces {
		if err := validateInterface(f, iface); err != nil {
			return nil, err
		}
		var w bytes.Buffer
		writeCPPPrologue(&w, f, cppNeeds(nil, []*Interface{iface}))
		writeCPPOpen(&w, f)
		emitCPPInterface(&w, iface)
		writeCPPClose(&w, f)
		out[snakeCase(iface.Name)+"_zap.hpp"] = w.Bytes()
	}
	return out, nil
}

// EmitCPPSingle emits one header holding every struct and interface in f.
// Returns the basename and the bytes.
func EmitCPPSingle(f *File) (string, []byte, error) {
	if len(f.Structs) == 0 && len(f.Interfaces) == 0 {
		return "", nil, fmt.Errorf("no structs or interfaces to emit")
	}
	for _, s := range f.Structs {
		if err := validate(s); err != nil {
			return "", nil, err
		}
	}
	for _, iface := range f.Interfaces {
		if err := validateInterface(f, iface); err != nil {
			return "", nil, err
		}
	}
	var w bytes.Buffer
	writeCPPPrologue(&w, f, cppNeeds(f.Structs, f.Interfaces))
	writeCPPOpen(&w, f)
	writeCPPForwards(&w, f.Structs)
	for _, s := range f.Structs {
		emitCPPStruct(&w, s)
	}
	emitCPPOutOfLine(&w, f.Structs)
	for _, iface := range f.Interfaces {
		emitCPPInterface(&w, iface)
	}
	writeCPPClose(&w, f)
	return stem(sourceName(f)) + "_zap.hpp", w.Bytes(), nil
}

// stem strips the final extension from a filename.
func stem(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[:i]
		}
	}
	return name
}

// --- includes ---------------------------------------------------------------

// needs records which headers the body will use. C++ tolerates an unused
// include; an include block that names exactly what the file uses is the honest
// report of the file's dependencies.
type needs struct {
	structs bool // any struct: <zap/zap.hpp>, <expected>, <span>, <vector>, <cstdint>
	rpc     bool // any interface: <zap/rpc.hpp>, <string>
	fixed   bool // bytes_fixed: <array>
	text    bool // text: <string_view>
	bits    bool // f32 / f64: <bit>
}

func cppNeeds(structs []*Struct, ifaces []*Interface) needs {
	var n needs
	n.structs = len(structs) > 0
	n.rpc = len(ifaces) > 0
	for _, s := range structs {
		for _, f := range s.Fields {
			switch f.Type.Kind {
			case KindBytesFixed:
				n.fixed = true
			case KindText:
				n.text = true
			case KindF32, KindF64:
				n.bits = true
			}
		}
	}
	return n
}

func writeCPPPrologue(w *bytes.Buffer, f *File, n needs) {
	w.WriteString("// Code generated by zapgen; DO NOT EDIT.\n")
	fmt.Fprintf(w, "// source: %s\n", sourceName(f))
	w.WriteString("// SPDX-License-Identifier: BSD-3-Clause-Eco\n\n")
	w.WriteString("#pragma once\n\n")
	if n.fixed {
		w.WriteString("#include <array>\n")
	}
	if n.bits {
		w.WriteString("#include <bit>\n")
	}
	w.WriteString("#include <cstdint>\n")
	w.WriteString("#include <expected>\n")
	w.WriteString("#include <span>\n")
	if n.rpc {
		w.WriteString("#include <string>\n")
	}
	if n.text {
		w.WriteString("#include <string_view>\n")
	}
	w.WriteString("#include <vector>\n\n")
	w.WriteString("#include <zap/zap.hpp>\n")
	if n.rpc {
		w.WriteString("#include <zap/rpc.hpp>\n")
	}
}

// writeCPPIncludes names the sibling headers a per-struct file depends on: one
// per nested struct it reads. Single-file output needs none — every class is
// already in the file.
func writeCPPIncludes(w *bytes.Buffer, f *File, s *Struct) {
	seen := map[string]bool{s.Name: true}
	var names []string
	for _, fd := range s.Fields {
		if fd.Type.Kind == KindStruct && !seen[fd.Type.StructName] {
			seen[fd.Type.StructName] = true
			names = append(names, fd.Type.StructName)
		}
	}
	if len(names) == 0 {
		return
	}
	w.WriteString("\n")
	for _, name := range names {
		fmt.Fprintf(w, "#include %q\n", snakeCase(name)+"_zap.hpp")
	}
}

func writeCPPOpen(w *bytes.Buffer, f *File) {
	fmt.Fprintf(w, "\nnamespace %s {\n\n", f.Package)
}

func writeCPPClose(w *bytes.Buffer, f *File) {
	fmt.Fprintf(w, "}  // namespace %s\n", f.Package)
}

// writeCPPForwards declares every class up front so a struct may name one
// declared later in the schema. The accessors that RETURN a class are defined
// out of line (see emitCPPOutOfLine) for the same reason: a forward
// declaration is enough to declare them, and by the time they are defined
// every class is complete. Declaration order in the schema then carries no
// meaning, and a cycle between two structs is expressible.
func writeCPPForwards(w *bytes.Buffer, structs []*Struct) {
	if len(structs) == 0 {
		return
	}
	for _, s := range structs {
		fmt.Fprintf(w, "class %s;\n", s.Name)
	}
	w.WriteString("\n")
}

// --- structs ----------------------------------------------------------------

func emitCPPStruct(w *bytes.Buffer, s *Struct) {
	emitCPPOffsets(w, s)
	emitCPPReader(w, s)
	emitCPPBuilder(w, s)
	w.WriteString("\n")
}

func emitCPPOffsets(w *bytes.Buffer, s *Struct) {
	fmt.Fprintf(w, "// Field byte offsets for %s, and the size of its fixed section.\n", s.Name)
	for _, f := range s.Fields {
		fmt.Fprintf(w, "inline constexpr std::int64_t %s = %d;\n", cppOffsetName(s, f), f.Offset)
	}
	fmt.Fprintf(w, "inline constexpr std::int64_t %s = %d;\n\n", cppSizeName(s), structSize(s))
}

func cppOffsetName(s *Struct, f *Field) string { return "k" + s.Name + f.Name + "Off" }
func cppSizeName(s *Struct) string             { return "k" + s.Name + "Size" }

func emitCPPReader(w *bytes.Buffer, s *Struct) {
	fmt.Fprintf(w, "// %s is a zero-copy view into a ZAP-encoded %s message. It borrows the\n", s.Name, s.Name)
	w.WriteString("// bytes it was built over, which must outlive it, and copies no field.\n")
	fmt.Fprintf(w, "class %s {\n", s.Name)
	w.WriteString("  public:\n")
	fmt.Fprintf(w, "    %s() = default;\n", s.Name)
	fmt.Fprintf(w, "    explicit %s(zap::Object o) : o_(o) {}\n\n", s.Name)
	w.WriteString("    bool is_null() const { return o_.is_null(); }\n")
	w.WriteString("    zap::Object object() const { return o_; }\n\n")
	for _, f := range s.Fields {
		emitCPPFieldReader(w, s, f)
	}
	w.WriteString("\n  private:\n")
	w.WriteString("    zap::Object o_;\n")
	w.WriteString("};\n\n")

	fmt.Fprintf(w, "// Wrap%s parses b and returns a typed view over it, or says why b is not a\n", s.Name)
	w.WriteString("// message. The view borrows b.\n")
	fmt.Fprintf(w, "inline std::expected<%s, zap::Error> Wrap%s(std::span<const std::uint8_t> b) {\n", s.Name, s.Name)
	w.WriteString("    auto m = zap::Message::parse(b);\n")
	w.WriteString("    if (!m) return std::unexpected(m.error());\n")
	fmt.Fprintf(w, "    return %s(m->root());\n", s.Name)
	w.WriteString("}\n\n")
}

func emitCPPFieldReader(w *bytes.Buffer, s *Struct, f *Field) {
	off := cppOffsetName(s, f)
	switch f.Type.Kind {
	case KindBool:
		fmt.Fprintf(w, "    bool %s() const { return o_.boolean(%s); }\n", f.Name, off)
	case KindU8:
		fmt.Fprintf(w, "    std::uint8_t %s() const { return o_.u8(%s); }\n", f.Name, off)
	case KindU16:
		fmt.Fprintf(w, "    std::uint16_t %s() const { return o_.u16(%s); }\n", f.Name, off)
	case KindU32:
		fmt.Fprintf(w, "    std::uint32_t %s() const { return o_.u32(%s); }\n", f.Name, off)
	case KindU64:
		fmt.Fprintf(w, "    std::uint64_t %s() const { return o_.u64(%s); }\n", f.Name, off)
	case KindI8:
		fmt.Fprintf(w, "    std::int8_t %s() const { return static_cast<std::int8_t>(o_.u8(%s)); }\n", f.Name, off)
	case KindI16:
		fmt.Fprintf(w, "    std::int16_t %s() const { return static_cast<std::int16_t>(o_.u16(%s)); }\n", f.Name, off)
	case KindI32:
		fmt.Fprintf(w, "    std::int32_t %s() const { return static_cast<std::int32_t>(o_.u32(%s)); }\n", f.Name, off)
	case KindI64:
		fmt.Fprintf(w, "    std::int64_t %s() const { return static_cast<std::int64_t>(o_.u64(%s)); }\n", f.Name, off)
	case KindF32:
		fmt.Fprintf(w, "    float %s() const { return std::bit_cast<float>(o_.u32(%s)); }\n", f.Name, off)
	case KindF64:
		fmt.Fprintf(w, "    double %s() const { return std::bit_cast<double>(o_.u64(%s)); }\n", f.Name, off)
	case KindText:
		fmt.Fprintf(w, "    std::string_view %s() const { return o_.text(%s); }\n", f.Name, off)
	case KindBytes:
		fmt.Fprintf(w, "    std::span<const std::uint8_t> %s() const { return o_.bytes(%s); }\n", f.Name, off)
	case KindBytesFixed:
		// A fixed-width field is always N bytes wide, and a buffer too short to
		// hold it reads as zeros — the same answer the Go backend's [N]byte
		// gives, because copy() into a zero array leaves the array zero. Say it
		// here or the two languages disagree about every truncated message,
		// which is exactly what the corpus catches.
		fmt.Fprintf(w, "    std::span<const std::uint8_t> %s() const {\n", f.Name)
		fmt.Fprintf(w, "        static constexpr std::array<std::uint8_t, %d> zero{};\n", f.Type.FixedSize)
		fmt.Fprintf(w, "        const auto b = o_.bytes_fixed(%s, %d);\n", off, f.Type.FixedSize)
		fmt.Fprintf(w, "        return b.size() == %d ? b : std::span<const std::uint8_t>(zero);\n", f.Type.FixedSize)
		w.WriteString("    }\n")
	case KindList:
		fmt.Fprintf(w, "    zap::List %s() const { return o_.list(%s); }\n", f.Name, off)
	case KindStruct:
		// Declared here, defined by emitCPPOutOfLine once every class exists.
		fmt.Fprintf(w, "    %s %s() const;\n", f.Type.StructName, f.Name)
	}
}

// emitCPPOutOfLine defines the accessors that return another generated class.
// They are declared in-class and defined here, after every class in the file
// is complete, so schema declaration order does not constrain the header.
func emitCPPOutOfLine(w *bytes.Buffer, structs []*Struct) {
	first := true
	for _, s := range structs {
		for _, f := range s.Fields {
			if f.Type.Kind != KindStruct {
				continue
			}
			if first {
				w.WriteString("// Nested-struct accessors, defined once every class above is complete.\n")
				first = false
			}
			fmt.Fprintf(w, "inline %s %s::%s() const { return %s(o_.object(%s)); }\n",
				f.Type.StructName, s.Name, f.Name, f.Type.StructName, cppOffsetName(s, f))
		}
	}
	if !first {
		w.WriteString("\n")
	}
}

func emitCPPBuilder(w *bytes.Buffer, s *Struct) {
	fmt.Fprintf(w, "// %sInput collects the field values for New%s. A borrowed field is a view:\n", s.Name, s.Name)
	w.WriteString("// it must outlive the call, not the buffer the call returns.\n")
	fmt.Fprintf(w, "struct %sInput {\n", s.Name)
	for _, f := range s.Fields {
		fmt.Fprintf(w, "    %s %s{};\n", cppInputType(f.Type), f.Name)
	}
	w.WriteString("};\n\n")

	fmt.Fprintf(w, "// New%s writes a ZAP-encoded %s message into a fresh buffer and returns it.\n", s.Name, s.Name)
	fmt.Fprintf(w, "inline std::vector<std::uint8_t> New%s(const %sInput& in) {\n", s.Name, s.Name)
	fmt.Fprintf(w, "    zap::Builder b(256, %s);\n", cppBuilderVersion)
	fmt.Fprintf(w, "    auto ob = b.start_object(%s);\n", cppSizeName(s))
	for _, f := range s.Fields {
		emitCPPFieldWriter(w, s, f)
	}
	w.WriteString("    ob.finish_as_root();\n")
	w.WriteString("    return b.finish();\n")
	w.WriteString("}\n")
}

// cppInputType is the C++ type of one Input field. It is the type the Go
// backend uses, said in C++: a value for a scalar, a fixed array for
// bytes_fixed[N], and a view for anything the caller already holds bytes for.
func cppInputType(t Type) string {
	switch t.Kind {
	case KindBool:
		return "bool"
	case KindU8:
		return "std::uint8_t"
	case KindU16:
		return "std::uint16_t"
	case KindU32:
		return "std::uint32_t"
	case KindU64:
		return "std::uint64_t"
	case KindI8:
		return "std::int8_t"
	case KindI16:
		return "std::int16_t"
	case KindI32:
		return "std::int32_t"
	case KindI64:
		return "std::int64_t"
	case KindF32:
		return "float"
	case KindF64:
		return "double"
	case KindText:
		return "std::string_view"
	case KindBytes:
		return "std::span<const std::uint8_t>"
	case KindBytesFixed:
		// Exactly N bytes by construction, as Go's [N]byte is.
		return fmt.Sprintf("std::array<std::uint8_t, %d>", t.FixedSize)
	case KindList:
		// One pre-built entry per element, as Go's [][]byte is.
		return "std::vector<std::span<const std::uint8_t>>"
	case KindStruct:
		// A pre-built sub-buffer, as Go's []byte is.
		return "std::span<const std::uint8_t>"
	}
	return "std::span<const std::uint8_t>"
}

func emitCPPFieldWriter(w *bytes.Buffer, s *Struct, f *Field) {
	off := cppOffsetName(s, f)
	switch f.Type.Kind {
	case KindBool:
		fmt.Fprintf(w, "    ob.set_bool(%s, in.%s);\n", off, f.Name)
	case KindU8:
		fmt.Fprintf(w, "    ob.set_u8(%s, in.%s);\n", off, f.Name)
	case KindU16:
		fmt.Fprintf(w, "    ob.set_u16(%s, in.%s);\n", off, f.Name)
	case KindU32:
		fmt.Fprintf(w, "    ob.set_u32(%s, in.%s);\n", off, f.Name)
	case KindU64:
		fmt.Fprintf(w, "    ob.set_u64(%s, in.%s);\n", off, f.Name)
	case KindI8:
		fmt.Fprintf(w, "    ob.set_u8(%s, static_cast<std::uint8_t>(in.%s));\n", off, f.Name)
	case KindI16:
		fmt.Fprintf(w, "    ob.set_u16(%s, static_cast<std::uint16_t>(in.%s));\n", off, f.Name)
	case KindI32:
		fmt.Fprintf(w, "    ob.set_u32(%s, static_cast<std::uint32_t>(in.%s));\n", off, f.Name)
	case KindI64:
		fmt.Fprintf(w, "    ob.set_u64(%s, static_cast<std::uint64_t>(in.%s));\n", off, f.Name)
	case KindF32:
		fmt.Fprintf(w, "    ob.set_u32(%s, std::bit_cast<std::uint32_t>(in.%s));\n", off, f.Name)
	case KindF64:
		fmt.Fprintf(w, "    ob.set_u64(%s, std::bit_cast<std::uint64_t>(in.%s));\n", off, f.Name)
	case KindText:
		fmt.Fprintf(w, "    ob.set_text(%s, in.%s);\n", off, f.Name)
	case KindBytes:
		fmt.Fprintf(w, "    ob.set_bytes(%s, in.%s);\n", off, f.Name)
	case KindBytesFixed:
		fmt.Fprintf(w, "    ob.set_bytes_fixed(%s, std::span<const std::uint8_t>(in.%s));\n", off, f.Name)
	case KindList:
		// One entry per element: a 4-byte little-endian length, then the
		// payload. Both halves are runtime writes — add_u32 lays the length
		// word, add_bytes the payload — and the element COUNT set_list carries
		// is the caller's, not the byte count add_bytes accumulated. Same
		// bytes, same count word, as the Go backend's AddObjectBytes.
		v := lowerFirst(f.Name) + "_list"
		fmt.Fprintf(w, "    auto %s = b.start_list(0);\n", v)
		fmt.Fprintf(w, "    for (const auto& elem : in.%s) {\n", f.Name)
		fmt.Fprintf(w, "        %s.add_u32(static_cast<std::uint32_t>(elem.size()));\n", v)
		fmt.Fprintf(w, "        %s.add_bytes(elem);\n", v)
		w.WriteString("    }\n")
		fmt.Fprintf(w, "    ob.set_list(%s, %s.finish().first, static_cast<std::int64_t>(in.%s.size()));\n",
			off, v, f.Name)
	case KindStruct:
		fmt.Fprintf(w, "    if (!in.%s.empty()) {\n", f.Name)
		fmt.Fprintf(w, "        auto nested = b.start_object(static_cast<std::int64_t>(in.%s.size()));\n", f.Name)
		fmt.Fprintf(w, "        nested.set_bytes_fixed(0, in.%s);\n", f.Name)
		fmt.Fprintf(w, "        ob.set_object(%s, nested.finish());\n", off)
		w.WriteString("    }\n")
	}
}

// --- services ---------------------------------------------------------------

// emitCPPInterface emits the ordinal table, the channel, the client, the
// handler contract and the dispatch — the Go backend's four pieces, with the
// same names and the same ordinals, over the same envelope.
func emitCPPInterface(w *bytes.Buffer, iface *Interface) {
	emitCPPOrdinals(w, iface)
	emitCPPClient(w, iface)
	emitCPPServer(w, iface)
}

func emitCPPOrdinals(w *bytes.Buffer, iface *Interface) {
	fmt.Fprintf(w, "// Method ordinals for the %s service (stable 1-based wire ids).\n", iface.Name)
	for _, m := range iface.Methods {
		fmt.Fprintf(w, "inline constexpr std::uint32_t k%s = %d;\n", ordinalName(iface, m), m.Ordinal)
	}
	w.WriteString("\n")
}

func emitCPPClient(w *bytes.Buffer, iface *Interface) {
	fmt.Fprintf(w, "// %sChannel ships one Call envelope and awaits its correlated Response.\n", iface.Name)
	fmt.Fprintf(w, "class %sChannel {\n", iface.Name)
	w.WriteString("  public:\n")
	fmt.Fprintf(w, "    virtual ~%sChannel() = default;\n", iface.Name)
	w.WriteString("    virtual std::expected<zap::rpc::Response, std::string> Call(\n")
	w.WriteString("        std::span<const std::uint8_t> envelope) = 0;\n")
	w.WriteString("};\n\n")

	fmt.Fprintf(w, "// %sAnswer is one call's result: the call's own promise, so a later call can\n", iface.Name)
	w.WriteString("// pipeline off it, and the response body.\n")
	fmt.Fprintf(w, "struct %sAnswer {\n", iface.Name)
	w.WriteString("    zap::rpc::Promise promise{};\n")
	w.WriteString("    std::vector<std::uint8_t> body{};\n")
	w.WriteString("};\n\n")

	fmt.Fprintf(w, "// %sClient is a typed client for the %s service over a ZAP call channel.\n", iface.Name, iface.Name)
	w.WriteString("// Each call takes a fresh promise id from its session; the \"On\" form of a\n")
	w.WriteString("// method targets a prior call's promise, so the server chains them and the\n")
	w.WriteString("// dependent call ships without waiting for the first to round-trip.\n")
	fmt.Fprintf(w, "class %sClient {\n", iface.Name)
	w.WriteString("  public:\n")
	fmt.Fprintf(w, "    %sClient(%sChannel* ch, std::span<const std::uint8_t> capability)\n", iface.Name, iface.Name)
	w.WriteString("        : ch_(ch), cap_(capability.begin(), capability.end()) {}\n\n")
	for _, m := range iface.Methods {
		emitCPPClientMethod(w, iface, m)
	}
	w.WriteString("  private:\n")
	for _, m := range iface.Methods {
		emitCPPClientInvoke(w, iface, m)
	}
	fmt.Fprintf(w, "    %sChannel* ch_;\n", iface.Name)
	w.WriteString("    std::vector<std::uint8_t> cap_;\n")
	w.WriteString("    zap::rpc::Session sess_;\n")
	w.WriteString("};\n\n")
}

// cppAnswer is a client method's result type: the answer record when the
// method returns a payload, the bare promise when it does not.
func cppAnswer(iface *Interface, m *Method) string {
	if m.Response != nil {
		return fmt.Sprintf("std::expected<%sAnswer, std::string>", iface.Name)
	}
	return "std::expected<zap::rpc::Promise, std::string>"
}

func emitCPPClientMethod(w *bytes.Buffer, iface *Interface, m *Method) {
	mname := exportIdent(m.Name)
	ret := cppAnswer(iface, m)
	arg, payload := cppMethodArg(m)

	fmt.Fprintf(w, "    %s %s(%s) {\n", ret, mname, arg)
	fmt.Fprintf(w, "        return invoke%s(zap::rpc::kNoTarget, %s);\n", mname, payload)
	w.WriteString("    }\n\n")

	fmt.Fprintf(w, "    // %sOn issues %s as a dependent call pipelined on the answer of on: the\n", mname, mname)
	w.WriteString("    // server substitutes on's resolved result for this call's payload before\n")
	w.WriteString("    // dispatch, so it carries none of its own.\n")
	fmt.Fprintf(w, "    %s %sOn(zap::rpc::Promise on) {\n", ret, mname)
	fmt.Fprintf(w, "        return invoke%s(on.id, {});\n", mname)
	w.WriteString("    }\n\n")
}

// emitCPPClientInvoke emits the one place a method's envelope is built and
// shipped; both the plain and the pipelined form call it.
func emitCPPClientInvoke(w *bytes.Buffer, iface *Interface, m *Method) {
	mname := exportIdent(m.Name)
	fmt.Fprintf(w, "    %s invoke%s(std::uint32_t target, std::span<const std::uint8_t> payload) {\n",
		cppAnswer(iface, m), mname)
	w.WriteString("        const zap::rpc::Promise p = sess_.next();\n")
	w.WriteString("        zap::rpc::Call c;\n")
	fmt.Fprintf(w, "        c.method = k%s;\n", ordinalName(iface, m))
	w.WriteString("        c.promise_id = p.id;\n")
	w.WriteString("        c.target = target;\n")
	w.WriteString("        c.cap = cap_;\n")
	w.WriteString("        c.payload = payload;\n")
	w.WriteString("        auto resp = ch_->Call(zap::rpc::build_request(c));\n")
	w.WriteString("        if (!resp) return std::unexpected(resp.error());\n")
	w.WriteString("        if (resp->status != zap::rpc::kStatusOK) {\n")
	fmt.Fprintf(w, "            return std::unexpected(std::string(\"%s.%s: status \") +\n", iface.Name, mname)
	w.WriteString("                                   std::to_string(resp->status));\n")
	w.WriteString("        }\n")
	if m.Response != nil {
		fmt.Fprintf(w, "        return %sAnswer{p, std::vector<std::uint8_t>(resp->body.begin(), resp->body.end())};\n",
			iface.Name)
	} else {
		w.WriteString("        return p;\n")
	}
	w.WriteString("    }\n\n")
}

// cppMethodArg is the parameter list and the payload expression for a method's
// request: a view when it has one, nothing when it does not.
func cppMethodArg(m *Method) (arg, payload string) {
	if m.Request == nil {
		return "", "{}"
	}
	return "std::span<const std::uint8_t> " + m.Request.Name, m.Request.Name
}

func emitCPPServer(w *bytes.Buffer, iface *Interface) {
	fmt.Fprintf(w, "// %sHandler is the server contract for the %s service. Implement each\n", iface.Name, iface.Name)
	fmt.Fprintf(w, "// method, then route envelopes to it with Dispatch%s.\n", iface.Name)
	fmt.Fprintf(w, "class %sHandler {\n", iface.Name)
	w.WriteString("  public:\n")
	fmt.Fprintf(w, "    virtual ~%sHandler() = default;\n", iface.Name)
	for _, m := range iface.Methods {
		arg, _ := cppMethodArg(m)
		ret := "std::expected<void, std::string>"
		if m.Response != nil {
			ret = "std::expected<std::vector<std::uint8_t>, std::string>"
		}
		fmt.Fprintf(w, "    virtual %s %s(%s) = 0;\n", ret, exportIdent(m.Name), arg)
	}
	w.WriteString("};\n\n")

	fmt.Fprintf(w, "// Dispatch%s decodes a Call envelope, routes it by method ordinal to h and\n", iface.Name)
	w.WriteString("// returns the response envelope. An unknown ordinal answers StatusNotFound;\n")
	w.WriteString("// a handler that fails answers StatusInternal.\n")
	fmt.Fprintf(w, "inline std::expected<std::vector<std::uint8_t>, std::string> Dispatch%s(\n", iface.Name)
	fmt.Fprintf(w, "    %sHandler& h, std::span<const std::uint8_t> envelope) {\n", iface.Name)
	w.WriteString("    auto call = zap::rpc::parse_request(envelope);\n")
	w.WriteString("    if (!call) return std::unexpected(std::string(zap::describe(call.error())));\n")
	w.WriteString("    switch (call->method) {\n")
	for _, m := range iface.Methods {
		fmt.Fprintf(w, "        case k%s: {\n", ordinalName(iface, m))
		emitCPPDispatchCase(w, m)
		w.WriteString("        }\n")
	}
	w.WriteString("        default:\n")
	w.WriteString("            return zap::rpc::build_response(zap::rpc::kStatusNotFound, call->promise_id, {});\n")
	w.WriteString("    }\n")
	w.WriteString("}\n\n")
}

func emitCPPDispatchCase(w *bytes.Buffer, m *Method) {
	mname := exportIdent(m.Name)
	callArg := ""
	if m.Request != nil {
		callArg = "call->payload"
	}
	if m.Response != nil {
		fmt.Fprintf(w, "            auto body = h.%s(%s);\n", mname, callArg)
		w.WriteString("            if (!body) {\n")
		w.WriteString("                return zap::rpc::build_response(zap::rpc::kStatusInternal, call->promise_id, {});\n")
		w.WriteString("            }\n")
		w.WriteString("            return zap::rpc::build_response(zap::rpc::kStatusOK, call->promise_id, *body);\n")
	} else {
		fmt.Fprintf(w, "            if (!h.%s(%s)) {\n", mname, callArg)
		w.WriteString("                return zap::rpc::build_response(zap::rpc::kStatusInternal, call->promise_id, {});\n")
		w.WriteString("            }\n")
		w.WriteString("            return zap::rpc::build_response(zap::rpc::kStatusOK, call->promise_id, {});\n")
	}
}
