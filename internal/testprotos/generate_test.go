// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGeneratedUpToDate regenerates the checked-in packages using the
// go:generate directive in gen.go and checks that nothing changed.
func TestGeneratedUpToDate(t *testing.T) {
	src, err := os.ReadFile("gen.go")
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, line := range strings.Split(string(src), "\n") {
		if rest, ok := strings.CutPrefix(line, "//go:generate go run ../../cmd/cotorp "); ok {
			args = strings.Fields(rest)
		}
	}
	if args == nil {
		t.Fatal("go:generate directive not found")
	}
	out := t.TempDir()
	for i, a := range args {
		if a == "-go_out" {
			args[i+1] = out
		}
	}
	cmd := exec.Command("go", append([]string{"run", "../../cmd/cotorp"}, args...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cotorp: %v\n%s", err, b)
	}
	n := 0
	filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		rel, _ := filepath.Rel(out, path)
		want, _ := os.ReadFile(path)
		got, err := os.ReadFile(rel)
		if err != nil {
			t.Errorf("%s: missing; run go generate", rel)
		} else if !bytes.Equal(got, want) {
			t.Errorf("%s: out of date; run go generate", rel)
		}
		return nil
	})
	if n == 0 {
		t.Error("no files generated")
	}
}

// TestSelfContained checks that generated files declare no package-level
// helper functions (all logic lives in methods) and import only the
// standard library or other generated packages.
func TestSelfContained(t *testing.T) {
	files, err := filepath.Glob("*/*.pb.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no generated files: %v", err)
	}
	for _, name := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(p, ".") && !strings.HasPrefix(p, "github.com/canonical/cotorp/internal/testprotos/") {
				t.Errorf("%s imports non-stdlib package %q", name, p)
			}
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				t.Errorf("%s declares package-level function %s", name, fd.Name.Name)
			}
		}
	}
}
