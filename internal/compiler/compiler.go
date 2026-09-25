// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package compiler loads .proto files from import paths, links them and
// runs the Go generator.
package compiler

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/canonical/cotorp/internal/desc"
	"github.com/canonical/cotorp/internal/parser"
	"github.com/canonical/cotorp/internal/wkt"
)

// Compiler loads and links .proto files.
type Compiler struct {
	// ImportPaths are directories searched for imports, in order. The
	// bundled google/protobuf/*.proto files are searched last.
	ImportPaths []string

	files   map[string]*desc.File
	loading map[string]bool
}

// New returns a Compiler searching the given import paths.
func New(importPaths []string) *Compiler {
	return &Compiler{
		ImportPaths: importPaths,
		files:       map[string]*desc.File{},
		loading:     map[string]bool{},
	}
}

// ResolveInput converts a command-line file argument to its name relative
// to an import path, as protoc does.
func (c *Compiler) ResolveInput(arg string) (string, error) {
	if _, err := os.Stat(arg); err == nil {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return "", err
		}
		for _, ip := range c.ImportPaths {
			ipAbs, err := filepath.Abs(ip)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(ipAbs, abs)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.ToSlash(rel), nil
			}
		}
		return "", fmt.Errorf("%s: file does not reside within any path specified using -I", arg)
	}
	// Treat it as a name relative to the import paths.
	name := filepath.ToSlash(arg)
	if _, _, err := c.read(name); err != nil {
		return "", err
	}
	return name, nil
}

func (c *Compiler) read(name string) (string, string, error) {
	if path.IsAbs(name) || strings.Contains("/"+name+"/", "/../") {
		return "", "", fmt.Errorf("%s: import paths must be relative and must not contain \"..\"", name)
	}
	for _, ip := range c.ImportPaths {
		p := filepath.Join(ip, filepath.FromSlash(name))
		b, err := os.ReadFile(p)
		if err == nil {
			return string(b), p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
	}
	if b, err := fs.ReadFile(wkt.FS, name); err == nil {
		return string(b), name, nil
	}
	return "", "", fmt.Errorf("%s: file not found in import paths", name)
}

// Load parses and links the named file and its dependencies.
func (c *Compiler) Load(name string) (*desc.File, error) {
	if f, ok := c.files[name]; ok {
		return f, nil
	}
	if c.loading[name] {
		return nil, fmt.Errorf("%s: import cycle", name)
	}
	c.loading[name] = true
	defer delete(c.loading, name)

	src, _, err := c.read(name)
	if err != nil {
		return nil, err
	}
	ast, err := parser.Parse(name, src)
	if err != nil {
		return nil, err
	}
	deps := map[string]*desc.File{}
	for _, imp := range ast.Imports {
		if imp.Option {
			continue
		}
		d, err := c.Load(imp.Path)
		if err != nil {
			return nil, fmt.Errorf("%v\n  imported from %s", err, imp.Pos)
		}
		deps[imp.Path] = d
	}
	f, err := desc.Build(ast, deps)
	if err != nil {
		return nil, err
	}
	c.files[name] = f
	return f, nil
}
