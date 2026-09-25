// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"go/token"
	"path"
	"strings"
	"unicode"
)

// camelCase converts a protobuf identifier to a Go identifier using the
// same rules as protoc-gen-go, so generated names are familiar.
func camelCase(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.' && i+1 < len(s) && isLower(s[i+1]):
			// Skip over '.' in ".{{lowercase}}".
		case c == '.':
			b = append(b, '_')
		case c == '_' && (i == 0 || s[i-1] == '.'):
			// Convert initial '_' to ensure we start with a capital letter.
			b = append(b, 'X')
		case c == '_' && i+1 < len(s) && isLower(s[i+1]):
			// Skip over '_' in "_{{lowercase}}".
		case isDigit(c):
			b = append(b, c)
		default:
			if isLower(c) {
				c -= 'a' - 'A'
			}
			b = append(b, c)
			for ; i+1 < len(s) && isLower(s[i+1]); i++ {
				b = append(b, s[i+1])
			}
		}
	}
	return string(b)
}

func isLower(c byte) bool { return 'a' <= c && c <= 'z' }
func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// cleanPackageName converts a string to a valid Go package name.
func cleanPackageName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" || unicode.IsDigit(rune(s[0])) {
		s = "_" + s
	}
	if token.IsKeyword(s) {
		s = "_" + s
	}
	return s
}

// defaultPackageName derives a package name from an import path.
func defaultPackageName(importPath string) string {
	return cleanPackageName(path.Base(importPath))
}
