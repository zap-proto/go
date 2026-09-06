// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package idl

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

// Struct is one declared struct. Size is the author's stated width for the
// fixed section, and zero when the schema leaves it to the fields.
type Struct struct {
	Name   string
	Size   int
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

// Shape says how one list field's elements sit on the wire.
//
// It is DERIVED, never declared. A list of numbers is a run of numbers, a
// list of fixed-width byte runs is those runs back to back, a list of
// structs that are all fixed width is those payloads back to back, and a
// list of structs carrying a variable tail is a run of relative pointers to
// payloads written elsewhere. The element type already answers the question,
// so the schema never spells it and no two schemas can spell it differently.
type Shape uint8

const (
	ShapeNumber  Shape = iota // fixed-width numbers; stride is the width
	ShapeFixed                // bytes_fixed[N]; stride N
	ShapeInline               // struct with no tail; stride is its payload size
	ShapePointer              // struct with a tail; stride 4, one signed rel offset each
)

// ptrStride is the width of one relative pointer in a pointer list.
const ptrStride = 4

// Tail reports whether s carries anything outside its fixed payload — a
// bytes/text run, a list, or a pointer to another struct. A struct with a
// tail cannot live inline in a list, because its tail has nowhere to go
// between two neighbours; that is the whole of the Inline/Pointer question.
func (f *File) Tail(s *Struct) bool {
	for _, fd := range s.Fields {
		switch fd.Type.Kind {
		case KindBytes, KindText, KindList, KindStruct:
			return true
		}
	}
	return false
}

// Struct returns the named struct, or nil.
func (f *File) Struct(name string) *Struct {
	for _, s := range f.Structs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Shape classifies a list ELEMENT type.
func (f *File) Shape(elem Type) Shape {
	switch elem.Kind {
	case KindBytesFixed:
		return ShapeFixed
	case KindStruct:
		if s := f.Struct(elem.StructName); s != nil && f.Tail(s) {
			return ShapePointer
		}
		return ShapeInline
	}
	return ShapeNumber
}

// Stride is the per-element width of a list of elem, and the clamp a reader
// gets for free from the schema: length * stride must fit what is left of the
// buffer, so a lying length word is refused once instead of at every element.
func (f *File) Stride(elem Type) int {
	switch f.Shape(elem) {
	case ShapeFixed:
		return elem.FixedSize
	case ShapeInline:
		return StructSize(f.Struct(elem.StructName))
	case ShapePointer:
		return ptrStride
	}
	return elem.SlotSize()
}

// StructSize is the width of the fixed section: what the schema says, or where
// the last field ends when the schema says nothing. Nothing is rounded — the
// layout is the author's, and a record whose reserved width is wider than its
// fields fill says so with `struct Name @N`.
func StructSize(s *Struct) int {
	if s.Size > 0 {
		return s.Size
	}
	size := 0
	for _, f := range s.Fields {
		end := f.Offset + f.Type.SlotSize()
		if end > size {
			size = end
		}
	}
	return size
}
