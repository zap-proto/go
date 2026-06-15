// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// zapgen reads a .zap schema file and emits per-struct Go accessor +
// builder code that calls the github.com/zap-proto/go runtime.
//
// Usage:
//
//	zapgen vms/xvm/txs/schema.zap         # emit into same dir as input
//	zapgen -out ./gen schema.zap          # emit into specified dir
//
// Author intent: drop a `//go:generate zapgen schema.zap` line at the
// top of each consuming package and run `go generate ./...`.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var (
		outDir = flag.String("out", "", "output directory (default: input file's dir)")
		single = flag.Bool("single", false, "emit one combined <schema>_zap.go instead of per-struct files")
		suffix = flag.String("type-suffix", "", "append SUFFIX to every generated type name (e.g. -type-suffix=View)")
		target = flag.String("target", "go", "code target: go | ts")
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	input := flag.Arg(0)

	if err := run(input, *outDir, *single, *suffix, *target); err != nil {
		fmt.Fprintf(os.Stderr, "zapgen: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: zapgen [-out OUTDIR] [-single] [-type-suffix SUFFIX] [-target go|ts] SCHEMA.zap")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Reads a .zap schema and emits one <struct>_zap.go file per struct.")
	fmt.Fprintln(os.Stderr, "With -single, emits one combined <SCHEMA>_zap.go file.")
	fmt.Fprintln(os.Stderr, "With -type-suffix, appends SUFFIX to every generated type name.")
	fmt.Fprintln(os.Stderr, "With -target=ts, emits one <SCHEMA>_zap.ts file (View + Builder classes")
	fmt.Fprintln(os.Stderr, "over the @hanzo/zap runtime); the Go emitter is unchanged.")
}

func run(input, outDir string, single bool, typeSuffix, target string) error {
	src, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	file, err := Parse(input, src)
	if err != nil {
		return err
	}
	if typeSuffix != "" {
		for _, s := range file.Structs {
			s.Name += typeSuffix
		}
		// Patch nested-struct references to the renamed types.
		for _, s := range file.Structs {
			for _, f := range s.Fields {
				if f.Type.Kind == KindStruct {
					f.Type.StructName += typeSuffix
				}
				if f.Type.Kind == KindList && f.Type.ListElem != nil &&
					f.Type.ListElem.Kind == KindStruct {
					f.Type.ListElem.StructName += typeSuffix
				}
			}
		}
	}
	if outDir == "" {
		outDir = filepath.Dir(input)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	switch target {
	case "go":
		// fall through to the Go emitter below.
	case "ts":
		name, body, err := EmitTS(file)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outDir, name), body, 0o644)
	default:
		return fmt.Errorf("unknown target %q (want go|ts)", target)
	}
	if single {
		name, body, err := EmitSingle(file)
		if err != nil {
			return err
		}
		path := filepath.Join(outDir, name)
		return os.WriteFile(path, body, 0o644)
	}
	files, err := Emit(file)
	if err != nil {
		return err
	}
	for name, body := range files {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}
