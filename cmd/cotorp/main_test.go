// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelpersPath checks how -helpers resolves a directory under -go_out to
// an import path, in source_relative mode from the enclosing go.mod.
func TestHelpersPath(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.27\n")
	write("proto/foo/bar.proto", "syntax = \"proto3\";\npackage foo;\noption go_package = \"example.com/app/gen/foo\";\nmessage M { string s = 1; }\n")
	out := filepath.Join(dir, "gen")
	if err := run([]string{"-I", filepath.Join(dir, "proto"), "-go_out", out, "-paths", "source_relative", "-helpers", "internal/h", "foo/bar.proto"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "foo", "bar.pb.go"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `h "example.com/app/gen/internal/h"`; !strings.Contains(string(got), want) {
		t.Errorf("generated file does not import %s", want)
	}
	if _, err := os.Stat(filepath.Join(out, "internal", "h", "helpers.pb.go")); err != nil {
		t.Error(err)
	}

	// A full import path under the output directory, and one outside it.
	if imp, d, err := resolveHelpers("example.com/app/gen/x;y", out, "", false); err != nil || imp != "example.com/app/gen/x;y" || d != "x" {
		t.Errorf("full path: got %q, %q, %v", imp, d, err)
	}
	if _, _, err := resolveHelpers("example.com/other/x", out, "", false); err == nil {
		t.Error("path outside the module accepted")
	}
	// Import mode with -module: the output directory stands for the module.
	if imp, d, err := resolveHelpers("internal/h", out, "example.com/app", true); err != nil || imp != "example.com/app/internal/h" || d != "internal/h" {
		t.Errorf("import mode: got %q, %q, %v", imp, d, err)
	}
}
