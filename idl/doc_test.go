// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package idl

import (
	"strings"
	"testing"
)

// TestDocCapture pins the rule that decides what a comment documents.
//
// Every projection of a schema — a docs page, an OpenAPI summary, an MCP
// tool description — has the schema's own words or it has nothing. So what
// the parser keeps is not a convenience, it is the difference between one
// description and several that drift.
func TestDocCapture(t *testing.T) {
	src := `# A remark about the file. The blank line ends it.

# What this schema is for.
package demo

# What a Thing is.
#
# And a second paragraph about it.
struct Thing {
    # What Seq counts.
    Seq u64 @0
    Amt u64 @8  # a trailing remark, about the line to its left
    Tag u64 @16
}

struct Reply {
    Ok u64 @0
}

# What the service does.
interface Svc {
    # What this method answers.
    ask(req: Thing) returns (resp: Reply)

    tell(req: Thing)
}
`
	f, err := Parse("demo.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got, want := strings.Join(f.Doc, "\n"), "What this schema is for."; got != want {
		t.Errorf("package doc = %q, want %q (a blank line must end the preceding remark)", got, want)
	}

	thing := f.Structs[0]
	if got, want := strings.Join(thing.Doc, "\n"), "What a Thing is.\n\nAnd a second paragraph about it."; got != want {
		t.Errorf("struct doc = %q, want %q", got, want)
	}

	if got, want := strings.Join(thing.Fields[0].Doc, "\n"), "What Seq counts."; got != want {
		t.Errorf("field Seq doc = %q, want %q", got, want)
	}
	if d := thing.Fields[1].Doc; d != nil {
		t.Errorf("field Amt doc = %q, want none: a '#' after code documents nothing", d)
	}
	if d := thing.Fields[2].Doc; d != nil {
		t.Errorf("field Tag doc = %q, want none: the trailing remark on Amt must not carry down", d)
	}

	svc := f.Interfaces[0]
	if got, want := strings.Join(svc.Doc, "\n"), "What the service does."; got != want {
		t.Errorf("interface doc = %q, want %q", got, want)
	}
	if got, want := strings.Join(svc.Methods[0].Doc, "\n"), "What this method answers."; got != want {
		t.Errorf("method ask doc = %q, want %q", got, want)
	}
	if d := svc.Methods[1].Doc; d != nil {
		t.Errorf("method tell doc = %q, want none", d)
	}
}

// TestDocIsOptional keeps an undocumented schema parsing exactly as it did
// before comments were kept: silence in the schema is silence in the AST,
// which is what lets a projection tell "not documented" from "documented
// with an empty string".
func TestDocIsOptional(t *testing.T) {
	f, err := Parse("bare.zap", []byte("package bare\nstruct S { X u64 @0 }\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Doc != nil || f.Structs[0].Doc != nil || f.Structs[0].Fields[0].Doc != nil {
		t.Fatal("an undocumented schema must carry no doc")
	}
}
