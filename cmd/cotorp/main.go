// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Command cotorp compiles .proto files to Go code that marshals and
// unmarshals the protobuf wire format using only the Go standard library.
//
// Usage:
//
//	cotorp [flags] file.proto...
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/cotorp/internal/compiler"
	"github.com/canonical/cotorp/internal/desc"
	"github.com/canonical/cotorp/internal/gengo"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("cotorp", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: cotorp [flags] file.proto...\n\nflags:\n")
		fs.PrintDefaults()
	}
	var (
		importPaths listFlag
		mappings    listFlag
		out         = fs.String("go_out", "", "output directory for generated Go files (required)")
		paths       = fs.String("paths", "import", `output layout: "import" (by Go import path) or "source_relative"`)
		module      = fs.String("module", "", "Go module path prefix to strip from output paths in import mode")
		jsonNumbers = fs.Bool("json_enum_numbers", false, "write enum values as numbers instead of names in JSON output")
		jsonDiscard = fs.Bool("json_discard_unknown", false, "ignore unknown keys when decoding JSON instead of rejecting them")
		jsonHex     listFlag
		helpers     = fs.String("helpers", "", "generate the helper functions once as this Go package (import/path[;name]) and import it, instead of declaring them in every file")
	)
	fs.Var(&jsonHex, "json_hex", "encode this bytes field as hex instead of base64 in JSON, e.g. -json_hex pkg.Msg.trace_id (repeatable)")
	fs.Var(&importPaths, "I", "directory to search for imports (repeatable; default \".\")")
	fs.Var(&importPaths, "proto_path", "alias for -I")
	fs.Var(&mappings, "M", "map a .proto file to a Go import path: -M foo/bar.proto=example.com/bar[;name] (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		fs.Usage()
		return fmt.Errorf("-go_out is required")
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("no input files")
	}
	if *paths != "import" && *paths != "source_relative" {
		return fmt.Errorf("invalid -paths value %q", *paths)
	}
	if len(importPaths) == 0 {
		importPaths = listFlag{"."}
	}
	importMap := map[string]string{}
	for _, m := range mappings {
		k, v, ok := strings.Cut(m, "=")
		if !ok || k == "" || v == "" {
			return fmt.Errorf("invalid -M value %q; expected file.proto=import/path", m)
		}
		importMap[k] = v
	}

	c := compiler.New(importPaths)
	var files []*desc.File
	seen := map[string]bool{}
	for _, arg := range fs.Args() {
		name, err := c.ResolveInput(arg)
		if err != nil {
			return err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		f, err := c.Load(name)
		if err != nil {
			return err
		}
		files = append(files, f)
	}

	g := gengo.New(gengo.Options{
		Paths:              *paths,
		Module:             *module,
		ImportMap:          importMap,
		JSONEnumNumbers:    *jsonNumbers,
		JSONHex:            jsonHex,
		JSONDiscardUnknown: *jsonDiscard,
		Helpers:            *helpers,
	})
	outs, err := g.Generate(files)
	if err != nil {
		return err
	}
	for _, o := range outs {
		p := filepath.Join(*out, filepath.FromSlash(o.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, o.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}
