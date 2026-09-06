// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import "fmt"

// AST types for the .zap schema DSL.
//
// Single source file produces one File. The File carries a package name
// (`package foo` directive), a set of type aliases (`type sig96 =
// bytes_fixed[96]`), and a sequence of struct definitions.

// File is the parsed contents of one .zap source file.
type File struct {
	Package    string
	Source     string          // basename of the input .zap file, for the // source: header
	Aliases    map[string]Type // alias name → resolved type
	Structs    []*Struct
	Interfaces []*Interface
}

// Interface is one declared RPC service: a named set of methods whose
// ordinals are auto-assigned 1, 2, 3, … in declaration order.
type Interface struct {
	Name    string
	Methods []*Method
}

// Method is one service method. Ordinal is the 1-based wire id assigned by
// declaration order — appending a method never renumbers earlier ones, so
// an existing method's ordinal is stable for the life of the interface.
// Request is the inbound struct payload (nil if the param list is empty);
// Response is the returned struct payload (nil if there is no `returns` or
// it is empty). A ZAP method carries at most one struct payload per
// direction.
type Method struct {
	Name     string
	Ordinal  int
	Request  *Param
	Response *Param
}

// Param is one method parameter (`name: StructName`). The type is always a
// struct name — method payloads are ZAP structs.
type Param struct {
	Name       string
	StructName string
}

// Struct is one declared struct.
type Struct struct {
	Name   string
	Fields []*Field
}

// Field is one struct field. Offset is author-controlled (the @N
// annotation in the schema) and emitted as a generated constant.
type Field struct {
	Name   string
	Type   Type
	Offset int
}

// Type is the resolved type of a field. Exactly one of Kind / FixedSize
// (if Kind == KindBytesFixed) / ListElem (if Kind == KindList) /
// StructName (if Kind == KindStruct) carries the type detail.
type Type struct {
	Kind       TypeKind
	FixedSize  int    // bytes_fixed[N]
	ListElem   *Type  // list<T>
	StructName string // nested struct by name

	// Stride is how wide one element of a list<T> is, or 0 when the element
	// has no width the schema can state. It is filled in by Resolve, once,
	// so no backend has to work it out again — and no two of them can work
	// it out differently.
	Stride int
}

// TypeKind enumerates the schema's primitive type tags.
type TypeKind uint8

const (
	KindInvalid TypeKind = iota
	KindBool
	KindU8
	KindU16
	KindU32
	KindU64
	KindI8
	KindI16
	KindI32
	KindI64
	KindF32
	KindF64
	KindBytes      // variable-length bytes
	KindBytesFixed // bytes_fixed[N]
	KindText       // variable-length UTF-8
	KindList       // list<T>
	KindStruct     // nested struct
)

// String returns the schema name of the kind. Used in error messages.
func (k TypeKind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindU8:
		return "u8"
	case KindU16:
		return "u16"
	case KindU32:
		return "u32"
	case KindU64:
		return "u64"
	case KindI8:
		return "i8"
	case KindI16:
		return "i16"
	case KindI32:
		return "i32"
	case KindI64:
		return "i64"
	case KindF32:
		return "f32"
	case KindF64:
		return "f64"
	case KindBytes:
		return "bytes"
	case KindBytesFixed:
		return "bytes_fixed"
	case KindText:
		return "text"
	case KindList:
		return "list"
	case KindStruct:
		return "struct"
	}
	return "invalid"
}

// SlotSize returns the per-field byte width in the fixed section of an
// object. Variable-length tails (bytes/text/list) occupy {relOff
// uint32, length uint32} = 8 bytes; nested struct pointers occupy
// {relOff uint32} = 4 bytes; bytes_fixed[N] occupies N bytes inline.
func (t Type) SlotSize() int {
	switch t.Kind {
	case KindBool, KindU8, KindI8:
		return 1
	case KindU16, KindI16:
		return 2
	case KindU32, KindI32, KindF32:
		return 4
	case KindU64, KindI64, KindF64:
		return 8
	case KindBytesFixed:
		return t.FixedSize
	case KindBytes, KindText, KindList:
		return 8
	case KindStruct:
		return 4
	}
	return 0
}

// Resolve fills in what the schema says but does not spell: how wide one
// element of each list is.
//
// A list carries its elements one of two ways, and which one is a property of
// the ELEMENT, not a flag anyone writes:
//
//   - an element whose width is in the schema — a scalar, a bytes_fixed[N], a
//     struct whose every field is one of those — is written at that width,
//     back to back. That is a STRIDE list, and it is what every Lux chain
//     puts on the wire: a run of 72-byte outputs, of 96-byte inputs, of
//     20-byte addresses, of 4-byte indices.
//
//   - an element whose width is not in the schema — bytes, text, a struct
//     with a tail — is written behind a four-byte length, because nothing
//     else can say where the next one starts.
//
// No annotation decides this and none could: a schema that had to be told
// which shape it meant would be a schema that could be told wrong.
func Resolve(f *File) error {
	declared := make(map[string]*Struct, len(f.Structs))
	for _, s := range f.Structs {
		declared[s.Name] = s
	}
	for _, s := range f.Structs {
		for _, fd := range s.Fields {
			if fd.Type.Kind != KindList {
				continue
			}
			elem := fd.Type.ListElem
			if elem == nil {
				return fmt.Errorf("struct %s field %s: list of nothing", s.Name, fd.Name)
			}
			if elem.Kind == KindList {
				return fmt.Errorf("struct %s field %s: a list of lists has no shape on the wire", s.Name, fd.Name)
			}
			if elem.Kind == KindStruct {
				es, ok := declared[elem.StructName]
				if !ok {
					return fmt.Errorf("struct %s field %s: list of undeclared struct %s", s.Name, fd.Name, elem.StructName)
				}
				if inline(es) {
					fd.Type.Stride = structSize(es)
				}
				continue
			}
			fd.Type.Stride = width(*elem)
		}
	}
	return nil
}

// inline reports whether a struct is entirely its own bytes — every field a
// scalar or a bytes_fixed[N]. Such a struct is a record: copy its bytes and
// it is still itself, which is what lets a list hold a run of them.
//
// A field that points elsewhere (bytes, text, list, a nested struct) is not
// copyable that way. Its pointer is relative to where the pointer sits, so a
// record moved into a list would name whatever now lies at that distance.
func inline(s *Struct) bool {
	for _, f := range s.Fields {
		switch f.Type.Kind {
		case KindBytes, KindText, KindList, KindStruct:
			return false
		}
	}
	return len(s.Fields) > 0
}

// width is how many bytes a value of t occupies where it lies, for the types
// that lie somewhere: a scalar and a bytes_fixed[N]. Everything else answers
// 0, meaning "not stated here".
func width(t Type) int {
	switch t.Kind {
	case KindBool, KindU8, KindI8:
		return 1
	case KindU16, KindI16:
		return 2
	case KindU32, KindI32, KindF32:
		return 4
	case KindU64, KindI64, KindF64:
		return 8
	case KindBytesFixed:
		return t.FixedSize
	}
	return 0
}
