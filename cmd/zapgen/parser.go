// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Parse parses a .zap source file into a *File.
func Parse(filename string, src []byte) (*File, error) {
	p := &parser{
		src:      src,
		filename: filename,
		line:     1,
		file: &File{
			Source:  filepathBase(filename),
			Aliases: make(map[string]Type),
		},
	}
	return p.parseFile()
}

// filepathBase returns the final path element of name. Avoids the import
// of path/filepath in this file (kept tiny so the parser stays focused).
func filepathBase(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' || name[i] == '\\' {
			return name[i+1:]
		}
	}
	return name
}

type parser struct {
	src      []byte
	pos      int
	line     int
	filename string
	file     *File
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", p.filename, p.line, fmt.Sprintf(format, args...))
}

// skipSpace advances past whitespace and # comments.
func (p *parser) skipSpace() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\n':
			p.line++
			p.pos++
		case c == ' ' || c == '\t' || c == '\r':
			p.pos++
		case c == '#':
			// Comment to end of line.
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// peek returns the next rune without consuming it.
func (p *parser) peek() rune {
	if p.pos >= len(p.src) {
		return 0
	}
	r, _ := utf8.DecodeRune(p.src[p.pos:])
	return r
}

// peekKeyword reports whether the upcoming bytes match keyword followed
// by a non-identifier rune. Does not advance.
func (p *parser) peekKeyword(keyword string) bool {
	if p.pos+len(keyword) > len(p.src) {
		return false
	}
	if string(p.src[p.pos:p.pos+len(keyword)]) != keyword {
		return false
	}
	if p.pos+len(keyword) == len(p.src) {
		return true
	}
	next, _ := utf8.DecodeRune(p.src[p.pos+len(keyword):])
	return !isIdentRune(next)
}

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentRune(r rune) bool  { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// readIdent reads an identifier. Returns ("", false) if no identifier
// is at the current position.
func (p *parser) readIdent() (string, bool) {
	start := p.pos
	if p.pos >= len(p.src) {
		return "", false
	}
	r, sz := utf8.DecodeRune(p.src[p.pos:])
	if !isIdentStart(r) {
		return "", false
	}
	p.pos += sz
	for p.pos < len(p.src) {
		r, sz = utf8.DecodeRune(p.src[p.pos:])
		if !isIdentRune(r) {
			break
		}
		p.pos += sz
	}
	return string(p.src[start:p.pos]), true
}

// readInt reads an unsigned integer.
func (p *parser) readInt() (int, error) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, p.errf("expected integer")
	}
	n, err := strconv.Atoi(string(p.src[start:p.pos]))
	if err != nil {
		return 0, p.errf("bad integer: %v", err)
	}
	return n, nil
}

// expect consumes a literal byte sequence; errors if it does not match.
func (p *parser) expect(lit string) error {
	if p.pos+len(lit) > len(p.src) || string(p.src[p.pos:p.pos+len(lit)]) != lit {
		return p.errf("expected %q", lit)
	}
	// Track newlines in the literal (rare, but be correct).
	for i := 0; i < len(lit); i++ {
		if lit[i] == '\n' {
			p.line++
		}
	}
	p.pos += len(lit)
	return nil
}

// parseFile is the top-level entry. Grammar:
//
//	File   := PackageDecl (TypeAlias | Struct)*
//	PackageDecl := 'package' Ident
func (p *parser) parseFile() (*File, error) {
	p.skipSpace()
	if !p.peekKeyword("package") {
		return nil, p.errf("expected `package` declaration")
	}
	p.pos += len("package")
	p.skipSpace()
	name, ok := p.readIdent()
	if !ok {
		return nil, p.errf("expected package name after `package`")
	}
	p.file.Package = name

	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		switch {
		case p.peekKeyword("struct"):
			s, err := p.parseStruct()
			if err != nil {
				return nil, err
			}
			p.file.Structs = append(p.file.Structs, s)
		case p.peekKeyword("type"):
			if err := p.parseAlias(); err != nil {
				return nil, err
			}
		default:
			return nil, p.errf("expected `struct` or `type` at top level")
		}
	}
	return p.file, nil
}

// parseAlias :=  'type' Ident '=' Type
func (p *parser) parseAlias() error {
	p.pos += len("type")
	p.skipSpace()
	name, ok := p.readIdent()
	if !ok {
		return p.errf("expected alias name after `type`")
	}
	p.skipSpace()
	if err := p.expect("="); err != nil {
		return err
	}
	p.skipSpace()
	t, err := p.parseType()
	if err != nil {
		return err
	}
	if _, dup := p.file.Aliases[name]; dup {
		return p.errf("duplicate type alias %q", name)
	}
	p.file.Aliases[name] = t
	return nil
}

// parseStruct := 'struct' Ident '{' Field* '}'
func (p *parser) parseStruct() (*Struct, error) {
	p.pos += len("struct")
	p.skipSpace()
	name, ok := p.readIdent()
	if !ok {
		return nil, p.errf("expected struct name")
	}
	p.skipSpace()
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	s := &Struct{Name: name}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, p.errf("unterminated struct %q", name)
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return s, nil
		}
		f, err := p.parseField()
		if err != nil {
			return nil, err
		}
		s.Fields = append(s.Fields, f)
	}
}

// parseField := Ident Type '@' Int
func (p *parser) parseField() (*Field, error) {
	name, ok := p.readIdent()
	if !ok {
		return nil, p.errf("expected field name")
	}
	p.skipSpace()
	t, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if err := p.expect("@"); err != nil {
		return nil, err
	}
	p.skipSpace()
	off, err := p.readInt()
	if err != nil {
		return nil, err
	}
	return &Field{Name: name, Type: t, Offset: off}, nil
}

// parseType parses one type expression.
func (p *parser) parseType() (Type, error) {
	if p.peekKeyword("list") {
		p.pos += len("list")
		p.skipSpace()
		if err := p.expect("<"); err != nil {
			return Type{}, err
		}
		p.skipSpace()
		inner, err := p.parseType()
		if err != nil {
			return Type{}, err
		}
		p.skipSpace()
		if err := p.expect(">"); err != nil {
			return Type{}, err
		}
		return Type{Kind: KindList, ListElem: &inner}, nil
	}
	if p.peekKeyword("bytes_fixed") {
		p.pos += len("bytes_fixed")
		p.skipSpace()
		if err := p.expect("["); err != nil {
			return Type{}, err
		}
		p.skipSpace()
		n, err := p.readInt()
		if err != nil {
			return Type{}, err
		}
		if n <= 0 {
			return Type{}, p.errf("bytes_fixed[N] must have N > 0")
		}
		p.skipSpace()
		if err := p.expect("]"); err != nil {
			return Type{}, err
		}
		return Type{Kind: KindBytesFixed, FixedSize: n}, nil
	}
	// Primitive keywords first; fall through to ident (alias or struct name).
	switch {
	case p.peekKeyword("bool"):
		p.pos += len("bool")
		return Type{Kind: KindBool}, nil
	case p.peekKeyword("u8"):
		p.pos += len("u8")
		return Type{Kind: KindU8}, nil
	case p.peekKeyword("u16"):
		p.pos += len("u16")
		return Type{Kind: KindU16}, nil
	case p.peekKeyword("u32"):
		p.pos += len("u32")
		return Type{Kind: KindU32}, nil
	case p.peekKeyword("u64"):
		p.pos += len("u64")
		return Type{Kind: KindU64}, nil
	case p.peekKeyword("i8"):
		p.pos += len("i8")
		return Type{Kind: KindI8}, nil
	case p.peekKeyword("i16"):
		p.pos += len("i16")
		return Type{Kind: KindI16}, nil
	case p.peekKeyword("i32"):
		p.pos += len("i32")
		return Type{Kind: KindI32}, nil
	case p.peekKeyword("i64"):
		p.pos += len("i64")
		return Type{Kind: KindI64}, nil
	case p.peekKeyword("f32"):
		p.pos += len("f32")
		return Type{Kind: KindF32}, nil
	case p.peekKeyword("f64"):
		p.pos += len("f64")
		return Type{Kind: KindF64}, nil
	case p.peekKeyword("bytes"):
		p.pos += len("bytes")
		return Type{Kind: KindBytes}, nil
	case p.peekKeyword("text"):
		p.pos += len("text")
		return Type{Kind: KindText}, nil
	}
	// User-defined name: alias or nested struct.
	name, ok := p.readIdent()
	if !ok {
		return Type{}, p.errf("expected type, got %q", p.peek())
	}
	if a, ok := p.file.Aliases[name]; ok {
		return a, nil
	}
	return Type{Kind: KindStruct, StructName: name}, nil
}
