// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package parser

import (
	"io/fs"
	"testing"

	"github.com/canonical/cotorp/internal/wkt"
)

func TestParseBundled(t *testing.T) {
	err := fs.WalkDir(wkt.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := fs.ReadFile(wkt.FS, path)
		if err != nil {
			return err
		}
		if _, err := Parse(path, string(src)); err != nil {
			t.Errorf("%v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
