// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"strings"
)

// Desugar rewrites whitespace-significant ZAP schema source into the
// canonical brace form the existing tokenizer/parser already accepts,
// then returns it. It runs BEFORE the parser so the proven brace parser
// stays untouched.
//
// The transform is intentionally a near-identity: it only ADDS the two
// tokens the brace grammar requires where the whitespace form omits them.
//
//   - A block header (`struct <Id>`) that is NOT already terminated by
//     '{' gets a trailing '{', and a matching '}' is inserted (at the
//     header's indent) where the indented body ends.
//   - A field written `Name Type` (no trailing '@N') gets `@<off>`
//     appended, where <off> is the running byte offset accumulated from
//     the declared slot width of the preceding fields in the same struct
//     (Cap'n-Proto-style positional assignment, here in this dialect's
//     byte-offset terms). An explicit '@N' is preserved and resets the
//     cursor to N + slotwidth.
//
// Consequence: a pure-brace file — every header ends in '{', every field
// carries '@N' — round-trips byte-for-byte unchanged, so brace fixtures
// keep parsing exactly as before. Styles may be mixed per top-level decl.
func Desugar(src []byte) ([]byte, error) {
	lines := splitLines(string(src))

	// First pass: collect `type X = ...` aliases so the offset cursor can
	// size aliased field types. Reuses the real type parser (one way to
	// size a type — no duplicated layout logic). Alias bodies are only
	// ever simple one-liners in this dialect, so a line scan suffices.
	aliases, err := collectAliases(lines)
	if err != nil {
		return nil, err
	}

	// File scope: no enclosing struct, hence a nil offset cursor.
	d := &desugarer{aliases: aliases}
	if err := d.emit(lines, 0, len(lines), -1, nil); err != nil {
		return nil, err
	}
	out := strings.Join(d.out, "\n")
	// Preserve the source's final-newline state so pure-brace files
	// round-trip byte-for-byte (splitLines dropped a single trailing "").
	if strings.HasSuffix(string(src), "\n") {
		out += "\n"
	}
	return []byte(out), nil
}

// line is one physical source line plus its derived facts.
type line struct {
	raw    string // the line verbatim, no trailing newline
	indent int    // count of leading spaces (tabs forbidden, see classify)
	body   string // raw with leading/trailing space trimmed
	kind   lineKind
}

type lineKind uint8

const (
	lineTransparent lineKind = iota // blank or full-line '#' comment
	lineOther                       // package, type alias, brace lines, etc.
	lineHeader                      // whitespace-mode block header (opens a block)
	lineField                       // whitespace-mode field (Name Type ...)
)

type desugarer struct {
	aliases map[string]string // alias name -> type expression text
	out     []string
}

// emit walks a contiguous run of lines, all of which belong to the block
// whose header sits at parentIndent. cursor (may be nil at file scope)
// is the byte-offset counter of the enclosing struct; each struct header
// recurses with a fresh cursor (offsets reset per struct).
func (d *desugarer) emit(lines []line, start, end, parentIndent int, cursor *int) error {
	i := start
	for i < end {
		ln := lines[i]
		if ln.kind == lineTransparent {
			d.out = append(d.out, ln.raw)
			i++
			continue
		}
		// A line that opens a literal brace block is brace syntax: copy it
		// and everything up to its matching '}' verbatim, so brace-style
		// declarations parse exactly as before (byte-identical).
		if braceDelta(ln.raw) > 0 {
			i = d.copyBraceRegion(lines, i, end)
			continue
		}
		switch ln.kind {
		case lineHeader:
			// Header opens a whitespace block: append '{', recurse over the
			// strictly-more-indented body, then close with '}' at this indent.
			// Strip any trailing comment so the brace lands after the header,
			// not inside the comment.
			d.out = append(d.out, indentSpaces(ln.indent)+stripComment(ln.body)+" {")
			bodyEnd := blockExtent(lines, i+1, end, ln.indent)
			childCursor := 0
			if err := d.emit(lines, i+1, bodyEnd, ln.indent, &childCursor); err != nil {
				return err
			}
			d.out = append(d.out, indentSpaces(ln.indent)+"}")
			i = bodyEnd
		case lineField:
			// At file scope (no enclosing struct header opened a block) a
			// "field" line cannot be desugared — there is no offset cursor.
			// Pass it through verbatim and let the parser report the precise
			// top-level error (e.g. an unrecognized header `structFoo` or a
			// bare `struct` followed by an indented body), rather than
			// crashing here with a misleading "outside a struct". [audit: M1]
			if cursor == nil {
				d.out = append(d.out, ln.raw)
				i++
				continue
			}
			out, err := d.field(ln, cursor)
			if err != nil {
				return err
			}
			d.out = append(d.out, out)
			i++
		default: // lineOther — pass through verbatim (brace syntax, package, alias).
			d.out = append(d.out, ln.raw)
			i++
		}
	}
	return nil
}

// copyBraceRegion copies lines verbatim starting at i (which opens at
// least one '{') until the running brace depth returns to zero, and
// returns the index one past the last copied line. Comments are ignored
// when counting braces.
func (d *desugarer) copyBraceRegion(lines []line, i, end int) int {
	depth := 0
	for i < end {
		d.out = append(d.out, lines[i].raw)
		depth += braceDelta(lines[i].raw)
		i++
		if depth <= 0 {
			break
		}
	}
	return i
}

// braceDelta returns the net '{' minus '}' on a line, ignoring any text
// after a '#' comment marker.
func braceDelta(raw string) int {
	d := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '#':
			return d
		case '{':
			d++
		case '}':
			d--
		}
	}
	return d
}

// field rewrites one whitespace-mode field line, assigning or honoring
// its '@N' byte offset and advancing *cursor. The caller guarantees a
// non-nil cursor (file-scope field lines are passed through verbatim in
// emit so the parser can report the precise top-level error). [audit: M1]
func (d *desugarer) field(ln line, cursor *int) (string, error) {
	name, typ, off, hasOff, err := splitField(ln.body)
	if err != nil {
		return "", err
	}
	width, err := d.slotWidth(typ)
	if err != nil {
		return "", fmt.Errorf("field %q: %w", name, err)
	}
	if !hasOff {
		off = *cursor
	}
	*cursor = off + width
	// Preserve the original indentation; emit canonical `Name Type @off`.
	return fmt.Sprintf("%s%s %s @%d", indentSpaces(ln.indent), name, typ, off), nil
}

// slotWidth returns the fixed-section byte width of the field type text,
// reusing the real type parser + SlotSize (single source of truth for
// layout — no duplicated sizing logic). A named alias is sized by its
// recorded body; list<T> and nested struct names are fixed-width
// pointers, which SlotSize already encodes.
func (d *desugarer) slotWidth(typ string) (int, error) {
	expr := typ
	if body, ok := d.aliases[typ]; ok {
		expr = body
	}
	tp := &parser{src: []byte(expr), file: &File{Aliases: map[string]Type{}}}
	t, err := tp.parseType()
	if err != nil {
		return 0, err
	}
	sz := t.SlotSize()
	if sz == 0 {
		return 0, fmt.Errorf("type %q has zero slot width", typ)
	}
	return sz, nil
}

// --- line classification -------------------------------------------------

// splitLines splits s on '\n', dropping a single trailing empty element
// so a file ending in '\n' round-trips to the same byte count.
func splitLines(s string) []line {
	parts := strings.Split(s, "\n")
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	out := make([]line, len(parts))
	for i, raw := range parts {
		out[i] = classify(raw)
	}
	return out
}

// classify derives the structural facts for one raw line.
func classify(raw string) line {
	ln := line{raw: raw, indent: leadingSpaces(raw), body: strings.TrimSpace(raw)}
	switch {
	case ln.body == "" || strings.HasPrefix(ln.body, "#"):
		ln.kind = lineTransparent
	case isWSHeader(ln.body):
		ln.kind = lineHeader
	case isWSField(ln.body):
		ln.kind = lineField
	default:
		ln.kind = lineOther
	}
	return ln
}

// leadingSpaces counts leading ASCII spaces. Tabs are treated as a
// single column each (kept simple — this dialect indents with spaces;
// any leading tab still yields a positive indent so blocks still nest).
func leadingSpaces(raw string) int {
	n := 0
	for n < len(raw) && (raw[n] == ' ' || raw[n] == '\t') {
		n++
	}
	return n
}

func indentSpaces(n int) string { return strings.Repeat(" ", n) }

// isWSHeader reports whether body is a whitespace-mode block header — a
// header that, in the brace grammar, would be followed by '{' but is not
// already terminated by '{'. In this Go dialect the only block opener is
// `struct <Id>`; a header already ending in '{' is brace syntax and is
// left to pass through as lineOther.
//
// A braceless header is EXACTLY `struct` + one identifier + end-of-line
// (after stripping a trailing '#' comment) — the analogue of the TS
// regex `^(struct|interface)[ \t]+[A-Za-z_]\w*[ \t]*$`. It deliberately
// does NOT match a field whose NAME is `struct` and that carries a type
// and/or '@offset' after it (e.g. `struct u8 @0`), nor `structFoo` (no
// space, one identifier — handled by afterKeyword), nor a brace header
// `struct S {`. Such lines fall through to isWSField and are desugared
// as ordinary fields. [audit: H4]
func isWSHeader(body string) bool {
	body = stripComment(body)
	if strings.HasSuffix(body, "{") {
		return false
	}
	rest, ok := afterKeyword(body, "struct")
	if !ok {
		return false
	}
	// Exactly one identifier may follow `struct`, with nothing after it:
	// `struct S` is a header; `struct u8 @0` (type/offset tail) is a field.
	id, tail := firstToken(rest)
	return isIdent(id) && tail == ""
}

// isIdent reports whether s is a single non-empty identifier
// (`[A-Za-z_]\w*`), matching the parser's identifier grammar.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_':
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false // identifiers cannot start with a digit
			}
		default:
			return false
		}
	}
	return true
}

// reservedHead names the one token that begins a file-scope construct
// (`package x`) and therefore can never be a field name. A real block
// header `struct X` is already claimed by isWSHeader before classify
// reaches isWSField, and a top-level alias `type x = …` is excluded by
// the '=' check below — so `struct`/`interface`/`type`/`enum`/… ARE
// valid field names (`struct u8 @0`, `interface text @8`, `type u8 @0`)
// and must classify as fields, not be swallowed as lineOther. [audit: H4]
var reservedHead = map[string]bool{
	"package": true,
}

// isWSField reports whether body is a whitespace-mode field declaration:
// at least `Name Type`, where Name is not a reserved file-scope keyword.
// Lines containing '{' or '}' or '=' (alias) are not fields.
func isWSField(body string) bool {
	if strings.ContainsAny(stripComment(body), "{}=") {
		return false
	}
	name, tail := firstToken(stripComment(body))
	if name == "" || tail == "" || reservedHead[name] {
		return false
	}
	typ, _ := firstToken(tail)
	return typ != ""
}

// --- parsing helpers (string-level, comment-aware) -----------------------

// stripComment removes a trailing '#' comment (and surrounding space).
func stripComment(body string) string {
	if i := strings.IndexByte(body, '#'); i >= 0 {
		return strings.TrimSpace(body[:i])
	}
	return body
}

// afterKeyword returns the text following keyword (with the separating
// space consumed) and whether body started with that keyword token.
func afterKeyword(body, kw string) (string, bool) {
	if !strings.HasPrefix(body, kw) {
		return "", false
	}
	rest := body[len(kw):]
	if rest == "" {
		return "", false // bare keyword, no identifier — not a header
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false // `structFoo` is one identifier, not the keyword
	}
	return strings.TrimSpace(rest), true
}

// firstToken splits leading whitespace-delimited token from the rest.
func firstToken(s string) (tok, rest string) {
	s = strings.TrimLeft(s, " \t")
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeft(s[i:], " \t")
}

// splitField parses a whitespace-mode field body into its parts. The
// type expression may itself contain spaces only inside `<...>` or
// `[...]`; in practice the dialect writes `list<T>` and `bytes_fixed[N]`
// without inner spaces, so the type is a single token. An explicit
// trailing `@N` (before any comment) is captured.
func splitField(body string) (name, typ string, off int, hasOff bool, err error) {
	body = stripComment(body)
	name, rest := firstToken(body)
	if name == "" || rest == "" {
		return "", "", 0, false, fmt.Errorf("malformed field %q", body)
	}
	typ, rest = firstToken(rest)
	if typ == "" {
		return "", "", 0, false, fmt.Errorf("field %q missing type", name)
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return name, typ, 0, false, nil
	}
	if !strings.HasPrefix(rest, "@") {
		return "", "", 0, false, fmt.Errorf("field %q: unexpected trailing %q", name, rest)
	}
	n, perr := atoiStrict(strings.TrimSpace(rest[1:]))
	if perr != nil {
		return "", "", 0, false, fmt.Errorf("field %q: bad @offset: %w", name, perr)
	}
	return name, typ, n, true, nil
}

// atoiStrict parses an unsigned decimal integer, rejecting anything else.
// It guards against overflow: an offset that exceeds maxOffset (or would
// overflow uint64) is rejected outright rather than silently wrapping —
// an unchecked accumulator turns @18446744073709551616 into 0, aliasing
// onto field offset 0 in the zero-copy layout. [audit: H2]
func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty integer")
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		d := uint64(c - '0')
		// Detect uint64 overflow before it happens.
		if n > (^uint64(0)-d)/10 {
			return 0, fmt.Errorf("offset %q out of range", s)
		}
		n = n*10 + d
	}
	if n > maxOffset {
		return 0, fmt.Errorf("offset %q out of range (max %d)", s, maxOffset)
	}
	return int(n), nil
}

// maxOffset bounds a field's declared byte offset. A ZAP struct's
// fixed section is addressed by these offsets in a zero-copy layout, so
// an offset far beyond any real message size is a typo, not a layout —
// rejecting it keeps parsing total (no int overflow, no silent wrap) and
// fits comfortably in a platform int on 32- and 64-bit builds alike.
const maxOffset = 1 << 31 // 2 GiB of fixed section — generous, never reached.

// blockExtent returns the index one past the last line belonging to a
// block whose header sits at headerIndent: the run of following lines
// whose indent > headerIndent, with transparent (blank/comment) lines
// absorbed so they neither open nor close a block.
func blockExtent(lines []line, start, end, headerIndent int) int {
	i := start
	for i < end {
		ln := lines[i]
		if ln.kind == lineTransparent {
			i++
			continue
		}
		if ln.indent <= headerIndent {
			break
		}
		i++
	}
	// Trim trailing transparent lines back out of the block so a blank
	// line between two top-level structs closes the first cleanly.
	for i > start && lines[i-1].kind == lineTransparent {
		i--
	}
	return i
}

// collectAliases scans for top-level `type X = <expr>` lines and records
// the RHS text so field offsets can size aliased types. A type alias is
// ONLY a top-level construct in this dialect (file scope = indent 0 and
// brace-depth 0), so a struct field literally named `type` (e.g.
// `type u8 @0`, whether under a whitespace header or inside `{ }`) is NOT
// scanned as an alias — it stays a field and is sized in the field path.
// Without this scope guard, collectAliases trips on the missing '=' and
// crashes the whole desugar before the parser is even reached. [audit: H3]
func collectAliases(lines []line) (map[string]string, error) {
	out := map[string]string{}
	depth := 0 // running brace depth, so brace-form struct bodies are skipped
	for _, ln := range lines {
		// A `type` line is a top-level alias only at file scope: not nested
		// inside a brace block and not indented under a whitespace header.
		topLevel := depth == 0 && ln.indent == 0
		depth += braceDelta(ln.raw)
		if !topLevel {
			continue
		}
		body := stripComment(ln.body)
		rest, ok := afterKeyword(body, "type")
		if !ok {
			continue
		}
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return nil, fmt.Errorf("alias %q missing '='", body)
		}
		name := strings.TrimSpace(rest[:eq])
		expr := strings.TrimSpace(rest[eq+1:])
		if name == "" || expr == "" {
			return nil, fmt.Errorf("malformed alias %q", body)
		}
		out[name] = expr
	}
	return out, nil
}
