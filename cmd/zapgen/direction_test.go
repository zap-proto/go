// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"strings"
	"testing"

	"github.com/zap-proto/go/idl"
)

// TestGeneratedDocsComeFromTheSchema is the direction check.
//
// Generation runs one way: the schema is edited, the output follows. The
// failure this pins is the reverse one — a description written into the
// generator (or into a docs page, or an OpenAPI summary) that the schema
// never said. That is how a schema stops being the source: the words live
// downstream, the shape lives upstream, and the two drift apart with
// nothing to notice.
//
// So: change the sentence in the schema, and the sentence in the output
// changes with it. Nothing else in this repository can produce that
// sentence, because it exists nowhere but the schema text below.
func TestGeneratedDocsComeFromTheSchema(t *testing.T) {
	const sentence = "Weighed at the height it was read at, never after."

	src := `# ` + sentence + `
package weigh

# ` + sentence + `
struct Mass {
    # ` + sentence + `
    Grams u64 @0
}

# ` + sentence + `
interface Scale {
    # ` + sentence + `
    weigh(req: Mass) returns (resp: Mass)
}
`
	f, err := idl.Parse("weigh.zap", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	files, err := Emit(f)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	for name, want := range map[string]int{
		"mass_zap.go":  3, // package doc, struct doc, field doc
		"scale_zap.go": 3, // package doc, interface doc, method doc
	} {
		src, ok := files[name]
		if !ok {
			t.Fatalf("no %s emitted (got %v)", name, keys(files))
		}
		if got := strings.Count(string(src), sentence); got != want {
			t.Errorf("%s: schema sentence appears %d times, want %d:\n%s", name, got, want, src)
		}
	}

	// The other direction, stated as a check: with the sentence removed from
	// the schema it must not survive anywhere in the output. If it did, the
	// generator — not the schema — would be the source of it.
	bare, err := idl.Parse("weigh.zap", []byte("package weigh\nstruct Mass { Grams u64 @0 }\n"))
	if err != nil {
		t.Fatalf("parse bare: %v", err)
	}
	files, err = Emit(bare)
	if err != nil {
		t.Fatalf("emit bare: %v", err)
	}
	for name, src := range files {
		if strings.Contains(string(src), sentence) {
			t.Errorf("%s carries a description the schema does not: %s", name, sentence)
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
