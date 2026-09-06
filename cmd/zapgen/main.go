// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// zapgen reads a .zap schema file and emits accessor + builder code that
// calls the ZAP runtime of the language asked for. One front end — one
// parser, one desugar, one schema model — and a backend per language.
// Every backend carries the same offsets, the same ordinals and the same
// bytes, because every backend reads one AST.
//
// Usage:
//
//	zapgen vms/xvm/txs/schema.zap         # Go, into the input's dir
//	zapgen -out ./gen schema.zap          # Go, into the given dir
//	zapgen -lang rust -out ./src s.zap    # Rust: the module + its runtime
//	zapgen -lang cpp -out ./gen s.zap     # C++ headers
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
		outDir  = flag.String("out", "", "output directory (default: input file's dir)")
		single  = flag.Bool("single", false, "emit one combined <schema>_zap file instead of per-struct files")
		suffix  = flag.String("type-suffix", "", "append SUFFIX to every generated type name (e.g. -type-suffix=View)")
		lang    = flag.String("lang", "go", "output language: go | rust | cpp")
		runtime = flag.String("rust-runtime", defaultRustRuntime, "Rust module path holding zap.rs and rpc.rs")
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	input := flag.Arg(0)

	if err := run(input, *outDir, *lang, *runtime, *single, *suffix); err != nil {
		fmt.Fprintf(os.Stderr, "zapgen: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: zapgen [-lang go|rust|cpp] [-out OUTDIR] [-single] [-type-suffix SUFFIX] SCHEMA.zap")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Reads a .zap schema and emits one file per struct and per interface:")
	fmt.Fprintln(os.Stderr, "  -lang go   <struct>_zap.go  calling github.com/zap-proto/go")
	fmt.Fprintln(os.Stderr, "  -lang cpp  <struct>_zap.hpp calling github.com/zap-proto/cpp")
	fmt.Fprintln(os.Stderr, "With -single, emits one combined <SCHEMA>_zap file instead.")
	fmt.Fprintln(os.Stderr, "With -type-suffix, appends SUFFIX to every generated type name.")
	fmt.Fprintln(os.Stderr, "With -lang rust, emits one <SCHEMA>_zap.rs module plus the")
	fmt.Fprintln(os.Stderr, "runtime it calls (zap.rs, and rpc.rs for a schema with an")
	fmt.Fprintln(os.Stderr, "interface). Rust compiles by module, so there is no per-struct")
	fmt.Fprintln(os.Stderr, "form and -single is implied.")
}

func run(input, outDir, lang, runtimePath string, single bool, typeSuffix string) error {
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
	files, err := emit(file, lang, runtimePath, single)
	if err != nil {
		return err
	}
	return write(outDir, files)
}

// emit runs one backend over one parsed file and returns what to write.
// Adding a language is adding a case here and an emitter beside emit.go —
// never a second parser.
func emit(f *File, lang, runtimePath string, single bool) (map[string][]byte, error) {
	switch lang {
	case "go":
		if single {
			return one(EmitSingle(f))
		}
		return Emit(f)
	case "cpp":
		if single {
			return one(EmitCPPSingle(f))
		}
		return EmitCPP(f)
	case "rust":
		return EmitRust(f, runtimePath)
	}
	return nil, fmt.Errorf("unknown -lang %q (want go, rust or cpp)", lang)
}

// one lifts a single-file emitter's result into the map every backend answers.
func one(name string, body []byte, err error) (map[string][]byte, error) {
	if err != nil {
		return nil, err
	}
	return map[string][]byte{name: body}, nil
}

// write puts every emitted file in dir.
func write(dir string, files map[string][]byte) error {
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}
