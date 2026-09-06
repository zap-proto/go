// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// zapgen reads a .zap schema file and emits accessor + builder code that
// calls the ZAP runtime of the language asked for. One front end — one
// parser, one desugar, one schema model — and a backend per language.
//
// Usage:
//
//	zapgen vms/xvm/txs/schema.zap         # Go, into the input's dir
//	zapgen -out ./gen schema.zap          # Go, into the given dir
//	zapgen -lang rust -out ./src s.zap    # Rust: the module + its runtime
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
		single  = flag.Bool("single", false, "emit one combined <schema>_zap.go instead of per-struct files")
		suffix  = flag.String("type-suffix", "", "append SUFFIX to every generated type name (e.g. -type-suffix=View)")
		lang    = flag.String("lang", "go", "output language: go or rust")
		runtime = flag.String("rust-runtime", defaultRustRuntime, "Rust module path holding zap.rs and rpc.rs")
		only    = flag.Bool("runtime", false, "write the Rust runtime alone, with no schema")
	)
	flag.Usage = usage
	flag.Parse()

	if *only {
		if flag.NArg() != 0 || *lang != "rust" {
			usage()
			os.Exit(2)
		}
		if err := writeRuntime(*outDir); err != nil {
			fmt.Fprintf(os.Stderr, "zapgen: %v\n", err)
			os.Exit(1)
		}
		return
	}
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
	fmt.Fprintln(os.Stderr, "usage: zapgen [-lang go|rust] [-out OUTDIR] [-single] [-type-suffix SUFFIX] SCHEMA.zap")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Reads a .zap schema and emits one <struct>_zap.go file per struct.")
	fmt.Fprintln(os.Stderr, "With -single, emits one combined <SCHEMA>_zap.go file.")
	fmt.Fprintln(os.Stderr, "With -type-suffix, appends SUFFIX to every generated type name.")
	fmt.Fprintln(os.Stderr, "With -lang rust, emits one <SCHEMA>_zap.rs module. Rust compiles by")
	fmt.Fprintln(os.Stderr, "module, so there is no per-struct form and -single is implied. The")
	fmt.Fprintln(os.Stderr, "runtime it calls travels beside it unless -rust-runtime names a crate")
	fmt.Fprintln(os.Stderr, "that already holds it; `-lang rust -runtime` writes that crate's copy.")
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
	switch lang {
	case "go":
	case "rust":
		files, err := EmitRust(file, runtimePath)
		if err != nil {
			return err
		}
		return write(outDir, files)
	default:
		return fmt.Errorf("unknown -lang %q (want go or rust)", lang)
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
	return write(outDir, files)
}

// writeRuntime puts the Rust runtime in dir and nothing else — what a crate
// that holds the runtime for several schemas is generated from.
func writeRuntime(dir string) error {
	if dir == "" {
		return fmt.Errorf("-runtime needs -out")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return write(dir, map[string][]byte{"zap.rs": rustRuntime, "rpc.rs": rustCallRuntime})
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
