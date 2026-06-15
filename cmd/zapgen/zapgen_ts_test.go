// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenTS parses every testdata/*.zap file, emits the combined TS via
// EmitTS, and diffs against the corresponding <schema>_zap.ts.golden fixture.
// Mirrors TestGolden (the Go golden test); run with -update to regenerate.
func TestGoldenTS(t *testing.T) {
	matches, err := filepath.Glob("testdata/*.zap")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no testdata/*.zap files")
	}
	for _, in := range matches {
		in := in
		t.Run(filepath.Base(in), func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatalf("read %s: %v", in, err)
			}
			file, err := Parse(in, src)
			if err != nil {
				t.Fatalf("parse %s: %v", in, err)
			}
			name, got, err := EmitTS(file)
			if err != nil {
				t.Fatalf("emit ts %s: %v", in, err)
			}
			goldenPath := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatalf("write golden %s: %v", goldenPath, err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to create)", goldenPath, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s: emitted TS does not match golden\n--- got ---\n%s\n--- want ---\n%s",
					goldenPath, indent(got), indent(want))
			}
		})
	}
}
