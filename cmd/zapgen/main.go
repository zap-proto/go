// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// zapgen reads a .zap schema file and emits per-struct accessor + builder
// code that calls a ZAP runtime.
//
// One front end, several back ends: the parser, the desugar and the schema
// model are shared, and -lang picks what gets printed. Go emits .go calling
// github.com/zap-proto/go; cpp emits .hpp calling github.com/zap-proto/cpp.
// Both carry the same offsets, the same ordinals and the same bytes, because
// both read one AST.
//
// Usage:
//
//	zapgen vms/xvm/txs/schema.zap         # emit Go into same dir as input
//	zapgen -out ./gen schema.zap          # emit into specified dir
//	zapgen -lang cpp -out ./gen s.zap     # emit C++ headers
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
		lang   = flag.String("lang", "go", "output language: go | cpp")
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	input := flag.Arg(0)

	if err := run(input, *outDir, *lang, *single, *suffix); err != nil {
		fmt.Fprintf(os.Stderr, "zapgen: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: zapgen [-lang go|cpp] [-out OUTDIR] [-single] [-type-suffix SUFFIX] SCHEMA.zap")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Reads a .zap schema and emits one file per struct and per interface:")
	fmt.Fprintln(os.Stderr, "  -lang go   <struct>_zap.go  calling github.com/zap-proto/go")
	fmt.Fprintln(os.Stderr, "  -lang cpp  <struct>_zap.hpp calling github.com/zap-proto/cpp")
	fmt.Fprintln(os.Stderr, "With -single, emits one combined <SCHEMA>_zap file instead.")
	fmt.Fprintln(os.Stderr, "With -type-suffix, appends SUFFIX to every generated type name.")
}

func run(input, outDir, lang string, single bool, typeSuffix string) error {
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
	emitSingle, emitAll, err := backend(lang)
	if err != nil {
		return err
	}
	if single {
		name, body, err := emitSingle(file)
		if err != nil {
			return err
		}
		path := filepath.Join(outDir, name)
		return os.WriteFile(path, body, 0o644)
	}
	files, err := emitAll(file)
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

// backend picks the pair of emitters for a language. Adding a language is
// adding a case here and an emitter beside emit.go — never a second parser.
func backend(lang string) (func(*File) (string, []byte, error), func(*File) (map[string][]byte, error), error) {
	switch lang {
	case "go":
		return EmitSingle, Emit, nil
	case "cpp":
		return EmitCPPSingle, EmitCPP, nil
	}
	return nil, nil, fmt.Errorf("unknown -lang %q (want go or cpp)", lang)
}
