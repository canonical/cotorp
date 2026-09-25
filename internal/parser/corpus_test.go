// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseCorpus parses every .proto under $COTORP_CORPUS, if set.
func TestParseCorpus(t *testing.T) {
	root := os.Getenv("COTORP_CORPUS")
	if root == "" {
		t.Skip("COTORP_CORPUS not set")
	}
	n := 0
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".proto" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n++
		if _, err := Parse(path, string(src)); err != nil {
			t.Errorf("%v", err)
		}
		return nil
	})
	t.Logf("parsed %d files", n)
}
