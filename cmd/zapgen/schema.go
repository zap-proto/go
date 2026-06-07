// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

// AST types for the .zap schema DSL.
//
// Single source file produces one File. The File carries a package name
// (`package foo` directive), a set of type aliases (`type sig96 =
// bytes_fixed[96]`), and a sequence of struct definitions.

// File is the parsed contents of one .zap source file.
type File struct {
	Package string
	Aliases map[string]Type // alias name → resolved type
	Structs []*Struct
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
