// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package parser

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Pos is a position in a source file.
type Pos struct {
	File string
	Line int
	Col  int
}

func (p Pos) String() string {
	if p.Line == 0 {
		return p.File
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// Error is a positioned parse or semantic error.
type Error struct {
	Pos Pos
	Msg string
}

func (e *Error) Error() string { return e.Pos.String() + ": " + e.Msg }

// Errorf returns a new *Error at pos.
func Errorf(pos Pos, format string, args ...any) error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokInt
	tokFloat
	tokString
	tokSymbol
)

func (k tokenKind) String() string {
	switch k {
	case tokEOF:
		return "end of file"
	case tokIdent:
		return "identifier"
	case tokInt:
		return "integer"
	case tokFloat:
		return "float"
	case tokString:
		return "string"
	default:
		return "symbol"
	}
}

type token struct {
	kind tokenKind
	// text is the raw token text. For strings it is the decoded value.
	text string
	pos  Pos
	// comments holds the leading comment attached to this token, if any.
	comments string
}

func (t token) describe() string {
	switch t.kind {
	case tokEOF:
		return "end of file"
	case tokString:
		return fmt.Sprintf("string %q", t.text)
	default:
		return fmt.Sprintf("%q", t.text)
	}
}

type lexer struct {
	file string
	src  string
	off  int
	line int
	col  int

	lastTokLine int
}

func tokenize(file, src string) ([]token, error) {
	l := &lexer{file: file, src: src, line: 1, col: 1}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tokEOF {
			return toks, nil
		}
	}
}

func (l *lexer) pos() Pos { return Pos{File: l.file, Line: l.line, Col: l.col} }

func (l *lexer) peekByte(n int) byte {
	if l.off+n < len(l.src) {
		return l.src[l.off+n]
	}
	return 0
}

func (l *lexer) advance() byte {
	c := l.src[l.off]
	l.off++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

// skipSpace skips whitespace and comments, returning the comment block
// immediately preceding the next token (detached comments and comments
// trailing the previous token are dropped).
func (l *lexer) skipSpace() (string, error) {
	var pending []string
	newlines := 0
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch {
		case c == '\n':
			newlines++
			if newlines >= 2 {
				pending = nil
			}
			l.advance()
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.advance()
		case c == '/' && l.peekByte(1) == '/':
			startLine := l.line
			l.advance()
			l.advance()
			start := l.off
			for l.off < len(l.src) && l.src[l.off] != '\n' {
				l.advance()
			}
			text := strings.TrimRight(l.src[start:l.off], "\r")
			if startLine == l.lastTokLine {
				// Trailing comment of the previous token.
				continue
			}
			if newlines >= 2 {
				pending = nil
			}
			newlines = 0
			pending = append(pending, text)
		case c == '/' && l.peekByte(1) == '*':
			startPos := l.pos()
			startLine := l.line
			l.advance()
			l.advance()
			start := l.off
			for {
				if l.off >= len(l.src) {
					return "", Errorf(startPos, "unterminated block comment")
				}
				if l.src[l.off] == '*' && l.peekByte(1) == '/' {
					break
				}
				l.advance()
			}
			body := l.src[start:l.off]
			l.advance()
			l.advance()
			if startLine == l.lastTokLine {
				continue
			}
			if newlines >= 2 {
				pending = nil
			}
			newlines = 0
			for _, line := range strings.Split(body, "\n") {
				line = strings.TrimRight(line, "\r")
				trimmed := strings.TrimLeft(line, " \t")
				if strings.HasPrefix(trimmed, "*") {
					line = trimmed[1:]
				}
				pending = append(pending, line)
			}
			// Drop leading/trailing empty lines from block comments.
			for len(pending) > 0 && strings.TrimSpace(pending[0]) == "" {
				pending = pending[1:]
			}
			for len(pending) > 0 && strings.TrimSpace(pending[len(pending)-1]) == "" {
				pending = pending[:len(pending)-1]
			}
		default:
			if newlines >= 2 {
				pending = nil
			}
			return strings.Join(pending, "\n"), nil
		}
	}
	return "", nil
}

func isLetter(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (l *lexer) next() (token, error) {
	comments, err := l.skipSpace()
	if err != nil {
		return token{}, err
	}
	p := l.pos()
	defer func() { l.lastTokLine = l.line }()
	if l.off >= len(l.src) {
		return token{kind: tokEOF, pos: p}, nil
	}
	c := l.src[l.off]
	start := l.off
	switch {
	case isLetter(c):
		for l.off < len(l.src) && (isLetter(l.src[l.off]) || isDigit(l.src[l.off])) {
			l.advance()
		}
		return token{kind: tokIdent, text: l.src[start:l.off], pos: p, comments: comments}, nil
	case isDigit(c) || (c == '.' && isDigit(l.peekByte(1))):
		kind := tokInt
		if c == '0' && (l.peekByte(1) == 'x' || l.peekByte(1) == 'X') {
			l.advance()
			l.advance()
			if !isHex(l.peekByte(0)) {
				return token{}, Errorf(p, "invalid hex literal")
			}
			for l.off < len(l.src) && isHex(l.src[l.off]) {
				l.advance()
			}
		} else {
			for l.off < len(l.src) && isDigit(l.src[l.off]) {
				l.advance()
			}
			if l.peekByte(0) == '.' {
				kind = tokFloat
				l.advance()
				for l.off < len(l.src) && isDigit(l.src[l.off]) {
					l.advance()
				}
			}
			if e := l.peekByte(0); e == 'e' || e == 'E' {
				kind = tokFloat
				l.advance()
				if s := l.peekByte(0); s == '+' || s == '-' {
					l.advance()
				}
				if !isDigit(l.peekByte(0)) {
					return token{}, Errorf(p, "invalid float literal")
				}
				for l.off < len(l.src) && isDigit(l.src[l.off]) {
					l.advance()
				}
			}
		}
		if l.off < len(l.src) && (isLetter(l.src[l.off]) || l.src[l.off] == '.') {
			return token{}, Errorf(p, "invalid numeric literal %q", l.src[start:l.off+1])
		}
		return token{kind: kind, text: l.src[start:l.off], pos: p, comments: comments}, nil
	case c == '"' || c == '\'':
		s, err := l.lexString()
		if err != nil {
			return token{}, err
		}
		return token{kind: tokString, text: s, pos: p, comments: comments}, nil
	default:
		l.advance()
		return token{kind: tokSymbol, text: string(c), pos: p, comments: comments}, nil
	}
}

func (l *lexer) lexString() (string, error) {
	p := l.pos()
	quote := l.advance()
	var b strings.Builder
	for {
		if l.off >= len(l.src) || l.src[l.off] == '\n' {
			return "", Errorf(p, "unterminated string literal")
		}
		c := l.advance()
		if c == quote {
			return b.String(), nil
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if l.off >= len(l.src) {
			return "", Errorf(p, "unterminated string literal")
		}
		ep := l.pos()
		e := l.advance()
		switch e {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '\\', '\'', '"', '?':
			b.WriteByte(e)
		case 'x', 'X':
			v, n := 0, 0
			for n < 2 && isHex(l.peekByte(0)) {
				v = v*16 + hexVal(l.advance())
				n++
			}
			if n == 0 {
				return "", Errorf(ep, "invalid hex escape")
			}
			b.WriteByte(byte(v))
		case 'u', 'U':
			want := 4
			if e == 'U' {
				want = 8
			}
			v := 0
			for i := 0; i < want; i++ {
				if !isHex(l.peekByte(0)) {
					return "", Errorf(ep, "invalid unicode escape")
				}
				v = v*16 + hexVal(l.advance())
			}
			if v > utf8.MaxRune {
				return "", Errorf(ep, "invalid unicode escape")
			}
			b.WriteRune(rune(v))
		default:
			if e >= '0' && e <= '7' {
				v := int(e - '0')
				for n := 1; n < 3 && l.peekByte(0) >= '0' && l.peekByte(0) <= '7'; n++ {
					v = v*8 + int(l.advance()-'0')
				}
				if v > 255 {
					return "", Errorf(ep, "octal escape out of range")
				}
				b.WriteByte(byte(v))
				continue
			}
			return "", Errorf(ep, "invalid escape sequence \\%c", e)
		}
	}
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
