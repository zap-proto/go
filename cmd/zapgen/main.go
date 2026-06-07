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
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	input := flag.Arg(0)

	if err := run(input, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "zapgen: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: zapgen [-out OUTDIR] SCHEMA.zap")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Reads a .zap schema and emits one <struct>_zap.go file per struct.")
}

func run(input, outDir string) error {
	src, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	file, err := Parse(input, src)
	if err != nil {
		return err
	}
	files, err := Emit(file)
	if err != nil {
		return err
	}
	if outDir == "" {
		outDir = filepath.Dir(input)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
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
