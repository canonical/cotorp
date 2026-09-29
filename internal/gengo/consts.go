// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"bytes"
	"strconv"
	"strings"
)

// Generated code refers to numbers with a fixed meaning, and to error
// messages used more than once, through unexported constants declared at the
// top of each file. Several files may share a Go package, so every constant
// name starts with a per-file prefix derived from the .proto base name (see
// constPrefix).

// constGroup is a set of related constants declared together.
type constGroup struct {
	doc    string // for readers of this file; not emitted
	consts []constDef
}

type constDef struct {
	suffix  string // name without the file prefix
	value   string
	comment string // optional explanation; not emitted
}

// constGroups lists every constant that generated code may use, in
// declaration order. Only the constants a file uses are declared.
var constGroups = []constGroup{
	{"Wire types.", []constDef{
		{"WireVarint", "0", ""},
		{"WireFixed64", "1", ""},
		{"WireBytes", "2", ""},
		{"WireStartGroup", "3", ""},
		{"WireEndGroup", "4", ""},
		{"WireFixed32", "5", ""},
	}},
	{"A tag holds the field number above the wire type in its low three bits.", []constDef{
		{"TagTypeBits", "3", ""},
		{"TagTypeMask", "1<<3 - 1", ""},
		{"MaxFieldNumber", "1<<29 - 1", ""},
	}},
	{"Map entries are encoded as messages with these field numbers.", []constDef{
		{"MapKeyField", "1", ""},
		{"MapValueField", "2", ""},
	}},
	{"Encoded sizes.", []constDef{
		{"Fixed32Size", "4", ""},
		{"Fixed64Size", "8", ""},
		{"VarintPayloadBits", "7", "value bits per varint byte"},
		{"VarintContBit", "0x80", "set on every varint byte but the last"},
	}},
	{"Decoding limits.", []constDef{
		{"MaxDepth", "10000", "maximum message and group nesting"},
		{"SkipStackSize", "16", "group nesting tracked without allocating"},
	}},
	{"ProtoJSON limits.", []constDef{
		{"MaxJSONExponent", "100", "bounds exact integer parsing of exponent forms"},
		{"Base64Quantum", "4", "base64 characters per padded block"},
		{"MaxFracDigits", "9", "fractional second digits in a Duration"},
		{"MinTimestampSeconds", "-62135596800", "0001-01-01T00:00:00Z"},
		{"MaxTimestampSeconds", "253402300799", "9999-12-31T23:59:59Z"},
		{"MaxDurationSeconds", "315576000000", "10000 years"},
	}},
	{"Time units.", []constDef{
		{"NanosPerSecond", "1000000000", ""},
		{"NanosPerMilli", "1000000", ""},
		{"NanosPerMicro", "1000", ""},
		{"MicrosPerSecond", "1000000", ""},
		{"MillisPerSecond", "1000", ""},
	}},
}

// constPrefix returns the lowerCamelCase base name of a .proto path, for use
// as the prefix of the file's constants: "google/protobuf/field_mask.proto"
// becomes "fieldMask".
func constPrefix(protoPath string) string {
	base := strings.TrimSuffix(protoPath[strings.LastIndexByte(protoPath, '/')+1:], ".proto")
	var b []byte
	upper := false
	for i := 0; i < len(base); i++ {
		c := base[i]
		switch {
		case isLower(c) || isDigit(c) || 'A' <= c && c <= 'Z':
			if upper && isLower(c) {
				c -= 'a' - 'A'
			}
			if len(b) == 0 && 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if len(b) == 0 && isDigit(c) {
				b = append(b, 'x')
			}
			b = append(b, c)
			upper = false
		default:
			upper = len(b) > 0
		}
	}
	if len(b) == 0 {
		return "file"
	}
	return string(b)
}

// constName returns a new package-level name prefix+suffix, adding a number
// if it is already taken in the package.
func (fg *fileGen) constName(suffix string) string {
	idents := fg.g.pkgIdents[fg.pkg.importPath]
	name := fg.prefix + suffix
	for i := 2; idents[name]; i++ {
		name = fg.prefix + suffix + strconv.Itoa(i)
	}
	idents[name] = true
	return name
}

// c returns the name of the constant with the given suffix from
// constGroups, declaring it in this file.
func (fg *fileGen) c(suffix string) string {
	if name, ok := fg.consts[suffix]; ok {
		return name
	}
	name := fg.constName(suffix)
	fg.consts[suffix] = name
	return name
}

// errConst is an error message used by generated code. It becomes a
// constant if it is used more than once, and a string literal otherwise.
type errConst struct {
	hint string // name suffix if it becomes a constant
	text string
	uses int
}

// errPlaceholder marks a use of fg.errs[i] in generated code until
// resolveErrs decides how each message is written.
const errPlaceholder = '\x00'

// errString records a use of error message text and returns a placeholder
// for its string expression. hint names the constant if one is declared;
// it should start with "Err".
func (fg *fileGen) errString(hint, text string) string {
	i, ok := fg.errIdx[text]
	if !ok {
		i = len(fg.errs)
		fg.errIdx[text] = i
		fg.errs = append(fg.errs, &errConst{hint: hint, text: text})
	}
	fg.errs[i].uses++
	return string(errPlaceholder) + strconv.Itoa(i) + string(errPlaceholder)
}

// owner returns an identifier fragment for a fully-qualified name in this
// file, used to name error constants: "pkg.Msg.field" becomes "MsgField".
func (fg *fileGen) owner(fullName string) string {
	if p := fg.f.Package; p != "" {
		fullName = strings.TrimPrefix(fullName, p+".")
	}
	var s string
	for _, part := range strings.Split(fullName, ".") {
		s += camelCase(part)
	}
	return s
}

// resolveErrs replaces error placeholders in src and returns the error
// constants to declare, as name and value pairs.
func (fg *fileGen) resolveErrs(src []byte) ([]byte, [][2]string) {
	repl := make([]string, len(fg.errs))
	var decls [][2]string
	for i, e := range fg.errs {
		if e.uses < 2 {
			repl[i] = strconv.Quote(e.text)
			continue
		}
		name := fg.constName(e.hint)
		repl[i] = name
		decls = append(decls, [2]string{name, strconv.Quote(e.text)})
	}
	var out []byte
	for {
		i := bytes.IndexByte(src, errPlaceholder)
		if i < 0 {
			return append(out, src...), decls
		}
		j := bytes.IndexByte(src[i+1:], errPlaceholder) + i + 1
		n, _ := strconv.Atoi(string(src[i+1 : j]))
		out = append(append(out, src[:i]...), repl[n]...)
		src = src[j+1:]
	}
}

// writeConsts writes the declarations of the constants this file uses, as
// one block. Group docs and comments are for readers of constGroups and are
// not emitted.
func (fg *fileGen) writeConsts(w *bytes.Buffer, errs [][2]string) {
	var lines []string
	for _, g := range constGroups {
		for _, c := range g.consts {
			if name, ok := fg.consts[c.suffix]; ok {
				lines = append(lines, name+" = "+c.value)
			}
		}
	}
	for _, e := range errs {
		lines = append(lines, e[0]+" = "+e[1])
	}
	switch len(lines) {
	case 0:
	case 1:
		w.WriteString("const " + lines[0] + "\n\n")
	default:
		w.WriteString("const (\n" + strings.Join(lines, "\n") + "\n)\n\n")
	}
}
