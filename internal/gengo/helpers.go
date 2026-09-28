// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

// Code that would otherwise repeat in every message is emitted once per file
// as an unexported function. As with constants, several files may share a Go
// package, so each function name starts with the file's prefix, and a file
// declares only the functions it uses.
//
// Helpers never take a message as an argument: calling a message's methods
// through an interface or type parameter would make messages escape to the
// heap. Methods call their own methods and pass the results to helpers.

// helperDef is a function that generated code may call.
type helperDef struct {
	suffix string // name without the file prefix
	gen    func(fg *fileGen, name string)
}

// helperDefs lists every helper, in declaration order.
var helperDefs = []helperDef{
	{"PutVarint", genPutVarint},
	{"PutFixed32", genPutFixed32},
	{"PutFixed64", genPutFixed64},
	{"PutBool", genPutBool},
	{"SizeLen", genSizeLen},
	{"ReadBytes", genReadBytes},
	{"SkipField", genSkipField},
	{"SkipGroup", genSkipGroup},
	{"Appended", genAppended},
	{"SortedKeys", genSortedKeys},
	{"MapSet", genMapSet},
	{"WriteJSON", genWriteJSON},
	{"AppendEnum", genAppendEnum},
	{"AppendFloat", genAppendFloat},
	{"CloseObject", genCloseObject},
	{"EndJSON", genEndJSON},
	{"StrictDecoder", genStrictDecoder},
	{"OpenJSON", genOpenJSON},
	{"ExpectJSON", genExpectJSON},
	{"EnumClass", genEnumClass},
	{"ParseInt", genParseInt},
	{"ParseUint", genParseUint},
	{"ExactInt", genExactInt},
	{"ParseFloat", genParseFloat},
	{"JSONNumber", genJSONNumber},
	{"ParseBool", genParseBool},
	{"ParseString", genParseString},
	{"ParseBytes", genParseBytes},
	{"ParseHex", genParseHex},
	{"ParseEnum", genParseEnum},
	{"ParseIntKey", genParseIntKey},
	{"ParseUintKey", genParseUintKey},
	{"ParseBoolKey", genParseBoolKey},
	{"JSONError", genJSONError},
}

// fn returns the name of the helper with the given suffix, declaring it in
// this file.
func (fg *fileGen) fn(suffix string) string {
	if name, ok := fg.funcs[suffix]; ok {
		return name
	}
	name := fg.constName(suffix)
	fg.funcs[suffix] = name
	return name
}

// genHelpers emits the helpers this file uses. A helper may use others, so
// generation repeats until no new helper is used.
func (fg *fileGen) genHelpers() {
	code := map[string]string{}
	for {
		added := false
		for _, h := range helperDefs {
			name, ok := fg.funcs[h.suffix]
			if !ok || code[h.suffix] != "" {
				continue
			}
			code[h.suffix] = fg.capture(func() { h.gen(fg, name) })
			added = true
		}
		if !added {
			break
		}
	}
	for _, h := range helperDefs {
		fg.buf.WriteString(code[h.suffix])
	}
}

// jsonErrorf returns an expression for the error "proto: <name>: <msg>"
// followed by the Go string expressions parts, where name is a Go string
// expression. msg becomes a constant if it is used more than once.
func (fg *fileGen) jsonErrorf(name, hint, msg string, parts ...string) string {
	e := fg.fn("JSONError") + "(" + name + ", " + fg.errString(hint, msg)
	for _, p := range parts {
		e += " + " + p
	}
	return e + ")"
}

// --- Binary -----------------------------------------------------------------

func genPutVarint(fg *fileGen, name string) {
	pb := fg.c("VarintPayloadBits")
	fg.P("// ", name, " writes u as a varint ending at b[i] and returns the index of")
	fg.P("// its first byte.")
	fg.P("func ", name, "(b []byte, i int, u uint64) int {")
	fg.P("if u < ", fg.c("VarintContBit"), " {")
	fg.P("b[i-1] = byte(u)")
	fg.P("return i - 1")
	fg.P("}")
	fg.P("i -= (", fg.std("math/bits"), ".Len64(u|1) + ", pb, " - 1) / ", pb)
	fg.P(fg.std("encoding/binary"), ".PutUvarint(b[i:], u)")
	fg.P("return i")
	fg.P("}")
	fg.P()
}

func genPutFixed32(fg *fileGen, name string) {
	fs := fg.c("Fixed32Size")
	fg.P("// ", name, " writes u in 4 little-endian bytes ending at b[i] and returns")
	fg.P("// the index of the first.")
	fg.P("func ", name, "(b []byte, i int, u uint32) int {")
	fg.P(fg.std("encoding/binary"), ".LittleEndian.PutUint32(b[i-", fs, ":], u)")
	fg.P("return i - ", fs)
	fg.P("}")
	fg.P()
}

func genPutFixed64(fg *fileGen, name string) {
	fs := fg.c("Fixed64Size")
	fg.P("// ", name, " writes u in 8 little-endian bytes ending at b[i] and returns")
	fg.P("// the index of the first.")
	fg.P("func ", name, "(b []byte, i int, u uint64) int {")
	fg.P(fg.std("encoding/binary"), ".LittleEndian.PutUint64(b[i-", fs, ":], u)")
	fg.P("return i - ", fs)
	fg.P("}")
	fg.P()
}

func genPutBool(fg *fileGen, name string) {
	fg.P("// ", name, " writes v as a one-byte varint ending at b[i] and returns its")
	fg.P("// index.")
	fg.P("func ", name, "(b []byte, i int, v bool) int {")
	fg.P("b[i-1] = 0")
	fg.P("if v {")
	fg.P("b[i-1] = 1")
	fg.P("}")
	fg.P("return i - 1")
	fg.P("}")
	fg.P()
}

func genSizeLen(fg *fileGen, name string) {
	fg.P("// ", name, " returns the size of a length-delimited value of l bytes,")
	fg.P("// including its length prefix.")
	fg.P("func ", name, "(l int) int {")
	fg.P("return l + ", fg.sizeVarint("uint64(l)"))
	fg.P("}")
	fg.P()
}

func genReadBytes(fg *fileGen, name string) {
	fg.P("// ", name, " returns the length-delimited value at the start of b and the")
	fg.P("// number of bytes it occupies, or n < 0 if it is malformed.")
	fg.P("func ", name, "(b []byte) (v []byte, n int) {")
	fg.P("ln, k := ", fg.std("encoding/binary"), ".Uvarint(b)")
	fg.P("if k <= 0 || ln > uint64(len(b)-k) {")
	fg.P("return nil, -1")
	fg.P("}")
	fg.P("return b[k : k+int(ln)], k + int(ln)")
	fg.P("}")
	fg.P()
}

func genSkipField(fg *fileGen, name string) {
	bin := fg.std("encoding/binary")
	fg.P("// ", name, " returns the length of the value at the start of b of a field")
	fg.P("// with tag t, in a message nested depth levels deep.")
	fg.P("func ", name, "(b []byte, t uint64, depth int) (int, error) {")
	fg.P("switch t & ", fg.c("TagTypeMask"), " {")
	fg.P("case ", fg.wire(wireVarint), ":")
	fg.P("if _, n := ", bin, ".Uvarint(b); n > 0 {")
	fg.P("return n, nil")
	fg.P("}")
	fg.P("case ", fg.wire(wireFixed64), ":")
	fg.P("if len(b) >= ", fg.c("Fixed64Size"), " {")
	fg.P("return ", fg.c("Fixed64Size"), ", nil")
	fg.P("}")
	fg.P("case ", fg.wire(wireBytes), ":")
	fg.P("if _, n := ", fg.fn("ReadBytes"), "(b); n >= 0 {")
	fg.P("return n, nil")
	fg.P("}")
	fg.P("case ", fg.wire(wireStartGroup), ":")
	fg.P("return ", fg.fn("SkipGroup"), "(b, int32(t>>", fg.c("TagTypeBits"), "), depth)")
	fg.P("case ", fg.wire(wireFixed32), ":")
	fg.P("if len(b) >= ", fg.c("Fixed32Size"), " {")
	fg.P("return ", fg.c("Fixed32Size"), ", nil")
	fg.P("}")
	fg.P("}")
	fg.P("return 0, ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("}")
	fg.P()
}

func genSkipGroup(fg *fileGen, name string) {
	tb := fg.c("TagTypeBits")
	fg.P("// ", name, " returns the length of the body of group num at the start of")
	fg.P("// b, including its end-group tag, in a message nested depth levels deep.")
	fg.P("// Nested groups are tracked with a small stack.")
	fg.P("func ", name, "(b []byte, num int32, depth int) (int, error) {")
	fg.P("var stk [", fg.c("SkipStackSize"), "]int32")
	fg.P("open := append(stk[:0], num)")
	fg.P("n := 0")
	fg.P("for len(open) > 0 {")
	fg.P("if depth+len(open) > ", fg.c("MaxDepth"), " {")
	fg.P("return 0, ", fg.errExpr("ErrDepth", errDepthMsg))
	fg.P("}")
	fg.P("t, k := ", fg.std("encoding/binary"), ".Uvarint(b[n:])")
	fg.P("if k <= 0 || t>>", tb, " == 0 || t>>", tb, " > ", fg.c("MaxFieldNumber"), " {")
	fg.P("return 0, ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("}")
	fg.P("n += k")
	fg.P("switch t & ", fg.c("TagTypeMask"), " {")
	fg.P("case ", fg.wire(wireStartGroup), ":")
	fg.P("open = append(open, int32(t>>", tb, "))")
	fg.P("case ", fg.wire(wireEndGroup), ":")
	fg.P("if open[len(open)-1] != int32(t>>", tb, ") {")
	fg.P("return 0, ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("}")
	fg.P("open = open[:len(open)-1]")
	fg.P("default:")
	fg.P("k, err := ", fg.fn("SkipField"), "(b[n:], t, depth)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("n += k")
	fg.P("}")
	fg.P("}")
	fg.P("return n, nil")
	fg.P("}")
	fg.P()
}

func genAppended(fg *fileGen, name string) {
	fg.P("// ", name, " finishes AppendBinary: b has capacity for size more bytes,")
	fg.P("// of which ProtoMarshalToSizedBuffer wrote n or failed with err.")
	fg.P("func ", name, "(b []byte, size, n int, err error) ([]byte, error) {")
	fg.P("if err == nil && n != size {")
	fg.P("err = ", fg.errExpr("ErrSizeChanged", errSizeMsg))
	fg.P("}")
	fg.P("if err != nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("return b[:len(b)+size], nil")
	fg.P("}")
	fg.P()
}

func genSortedKeys(fg *fileGen, name string) {
	fg.P("// ", name, " appends the keys of m to keys, which should be empty, and")
	fg.P("// sorts them. The caller allocates keys, so that it can stay on the stack.")
	fg.P("func ", name, "[K ", fg.std("cmp"), ".Ordered, V any](m map[K]V, keys []K) []K {")
	fg.P("for k := range m {")
	fg.P("keys = append(keys, k)")
	fg.P("}")
	fg.P(fg.std("slices"), ".Sort(keys)")
	fg.P("return keys")
	fg.P("}")
	fg.P()
}

func genMapSet(fg *fileGen, name string) {
	fg.P("// ", name, " sets (*m)[k] to v, allocating *m if it is nil.")
	fg.P("func ", name, "[K comparable, V any](m *map[K]V, k K, v V) {")
	fg.P("if *m == nil {")
	fg.P("*m = make(map[K]V)")
	fg.P("}")
	fg.P("(*m)[k] = v")
	fg.P("}")
	fg.P()
}

// --- JSON marshal -----------------------------------------------------------

func genWriteJSON(fg *fileGen, name string) {
	fg.P("// ", name, " finishes MarshalJSONTo: it writes the JSON value b to e,")
	fg.P("// unless producing b failed with err.")
	fg.P("func ", name, "(e *", fg.std("encoding/json/jsontext"), ".Encoder, b []byte, err error) error {")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("return e.WriteValue(b)")
	fg.P("}")
	fg.P()
}

func genAppendEnum(fg *fileGen, name string) {
	fg.P("// ", name, " appends enum value v as its name in names, or as a number")
	fg.P("// if it has none.")
	fg.P("func ", name, "(b []byte, v int32, names map[int32]string) []byte {")
	fg.P("if s, ok := names[v]; ok {")
	fg.P("b = append(b, '\"')")
	fg.P("b = append(b, s...)")
	fg.P("return append(b, '\"')")
	fg.P("}")
	fg.P("return ", fg.std("strconv"), ".AppendInt(b, int64(v), 10)")
	fg.P("}")
	fg.P()
}

func genAppendFloat(fg *fileGen, name string) {
	m := fg.std("math")
	fg.P("// ", name, " appends f, a float of the given bit size, as a JSON number or")
	fg.P("// as \"NaN\", \"Infinity\" or \"-Infinity\".")
	fg.P("func ", name, "(b []byte, f float64, bits int) []byte {")
	fg.P("switch {")
	fg.P("case ", m, ".IsNaN(f):")
	fg.P("return append(b, `\"NaN\"`...)")
	fg.P("case ", m, ".IsInf(f, 1):")
	fg.P("return append(b, `\"Infinity\"`...)")
	fg.P("case ", m, ".IsInf(f, -1):")
	fg.P("return append(b, `\"-Infinity\"`...)")
	fg.P("}")
	fg.P("return ", fg.std("encoding/json/jsontext"), ".AppendFloat(b, f, bits)")
	fg.P("}")
	fg.P()
}

func genCloseObject(fg *fileGen, name string) {
	fg.P("// ", name, " finishes a JSON object whose members were appended to b from")
	fg.P("// index start, each preceded by a comma: the first comma becomes the")
	fg.P("// opening brace.")
	fg.P("func ", name, "(b []byte, start int) []byte {")
	fg.P("if len(b) == start {")
	fg.P("return append(b, \"{}\"...)")
	fg.P("}")
	fg.P("b[start] = '{'")
	fg.P("return append(b, '}')")
	fg.P("}")
	fg.P()
}

// --- JSON unmarshal ---------------------------------------------------------

func genEndJSON(fg *fileGen, name string) {
	fg.P("// ", name, " finishes ProtoMergeJSON for message name: decoding one value")
	fg.P("// from d failed with err, or d must have no more data.")
	fg.P("func ", name, "(d *", fg.std("encoding/json/jsontext"), ".Decoder, err error, name string) error {")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if _, err := d.ReadToken(); err != ", fg.std("io"), ".EOF {")
	fg.P("return ", fg.jsonErrorf("name", "ErrTrailingData", "unexpected data after JSON value"))
	fg.P("}")
	fg.P("return nil")
	fg.P("}")
	fg.P()
}

func genStrictDecoder(fg *fileGen, name string) {
	jt := fg.std("encoding/json/jsontext")
	fg.P("// ", name, " returns d, or a strict decoder for the next value of d if d")
	fg.P("// replaces invalid UTF-8 (as encoding/json's decoder does), which")
	fg.P("// ProtoJSON rejects.")
	fg.P("func ", name, "(d *", jt, ".Decoder) (*", jt, ".Decoder, error) {")
	fg.P("if lax, _ := ", fg.std("encoding/json/v2"), ".GetOption(d.Options(), ", jt, ".AllowInvalidUTF8); !lax {")
	fg.P("return d, nil")
	fg.P("}")
	fg.P("v, err := d.ReadValue()")
	fg.P("if err != nil {")
	fg.P("return nil, err")
	fg.P("}")
	fg.P("return ", jt, ".NewDecoder(", fg.std("bytes"), ".NewBuffer(v)), nil")
	fg.P("}")
	fg.P()
}

func genOpenJSON(fg *fileGen, name string) {
	jt := fg.std("encoding/json/jsontext")
	fg.P("// ", name, " reads the first token of the value of message name, which")
	fg.P("// must be of the given kind. It reports false if the value is null, which")
	fg.P("// leaves the message unchanged, or on error.")
	fg.P("func ", name, "(d *", jt, ".Decoder, kind ", jt, ".Kind, name, what string) (bool, error) {")
	fg.P("if d.PeekKind() == ", fg.kind("Null"), " {")
	fg.P("return false, d.SkipValue()")
	fg.P("}")
	fg.P("err := ", fg.fn("ExpectJSON"), "(d, kind, name, what)")
	fg.P("return err == nil, err")
	fg.P("}")
	fg.P()
}

func genExpectJSON(fg *fileGen, name string) {
	jt := fg.std("encoding/json/jsontext")
	fg.P("// ", name, " reads a token of the given kind, a JSON what, in message name.")
	fg.P("func ", name, "(d *", jt, ".Decoder, kind ", jt, ".Kind, name, what string) error {")
	fg.P("tok, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if tok.Kind() != kind {")
	fg.P("return ", fg.jsonErrorf("name", "ErrExpected", "expected a JSON ", "what"))
	fg.P("}")
	fg.P("return nil")
	fg.P("}")
	fg.P()
}

func genEnumClass(fg *fileGen, name string) {
	fg.P("// ", name, " returns the parse class and bit size for an enum value of")
	fg.P("// the given token kind: nothing to parse for null, a number, or a name,")
	fg.P("// which stays in the token until it is looked up.")
	fg.P("func ", name, "(k ", fg.std("encoding/json/jsontext"), ".Kind) (int, int) {")
	fg.P("switch k {")
	fg.P("case ", fg.kind("Null"), ":")
	fg.P("return ", fg.class(classNone), ", 0")
	fg.P("case ", fg.kind("String"), ":")
	fg.P("return ", fg.class(classEnum), ", 0")
	fg.P("}")
	fg.P("return ", fg.class(classSigned), ", 32")
	fg.P("}")
	fg.P()
}

func genParseInt(fg *fileGen, name string) {
	m := fg.std("math")
	fg.P("// ", name, " parses tok, a number or a quoted number, as a signed integer")
	fg.P("// of the given bit size, for a field of message name.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, bits int, name string) (int64, error) {")
	fg.P("s, err := ", fg.fn("JSONNumber"), "(tok, name)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("if v, err := ", fg.std("strconv"), ".ParseInt(s, 10, bits); err == nil {")
	fg.P("return v, nil")
	fg.P("}")
	fg.P("n := ", fg.fn("ExactInt"), "(s)")
	fg.P("if n == nil || !n.IsInt64() || (bits == 32 && (n.Int64() < ", m, ".MinInt32 || n.Int64() > ", m, ".MaxInt32)) {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidInteger", "invalid integer ", "s"))
	fg.P("}")
	fg.P("return n.Int64(), nil")
	fg.P("}")
	fg.P()
}

func genParseUint(fg *fileGen, name string) {
	fg.P("// ", name, " parses tok, a number or a quoted number, as an unsigned")
	fg.P("// integer of the given bit size, for a field of message name.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, bits int, name string) (uint64, error) {")
	fg.P("s, err := ", fg.fn("JSONNumber"), "(tok, name)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("if v, err := ", fg.std("strconv"), ".ParseUint(s, 10, bits); err == nil {")
	fg.P("return v, nil")
	fg.P("}")
	fg.P("n := ", fg.fn("ExactInt"), "(s)")
	fg.P("if n == nil || !n.IsUint64() || (bits == 32 && n.Uint64() > ", fg.std("math"), ".MaxUint32) {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidInteger", "invalid integer ", "s"))
	fg.P("}")
	fg.P("return n.Uint64(), nil")
	fg.P("}")
	fg.P()
}

func genExactInt(fg *fileGen, name string) {
	conv := fg.std("strconv")
	max := fg.c("MaxJSONExponent")
	fg.P("// ", name, " returns the integer that JSON number s denotes exactly, in an")
	fg.P("// exponent or fraction form, or nil. The exponent is bounded so that exact")
	fg.P("// arithmetic stays cheap.")
	fg.P("func ", name, "(s string) *", fg.std("math/big"), ".Int {")
	fg.P("if i := ", fg.std("strings"), ".IndexAny(s, \"eE\"); i >= 0 {")
	fg.P("if e, err := ", conv, ".Atoi(s[i+1:]); err != nil || e > ", max, " || e < -", max, " {")
	fg.P("return nil")
	fg.P("}")
	fg.P("}")
	fg.P("r, ok := new(", fg.std("math/big"), ".Rat).SetString(s)")
	fg.P("if !ok || !r.IsInt() {")
	fg.P("return nil")
	fg.P("}")
	fg.P("return r.Num()")
	fg.P("}")
	fg.P()
}

func genParseFloat(fg *fileGen, name string) {
	m := fg.std("math")
	fg.P("// ", name, " parses tok, a number, a quoted number, or \"NaN\", \"Infinity\"")
	fg.P("// or \"-Infinity\", as a float of the given bit size, for a field of")
	fg.P("// message name.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, bits int, name string) (float64, error) {")
	fg.P("if tok.Kind() == ", fg.kind("String"), " {")
	fg.P("switch tok.String() {")
	fg.P("case \"NaN\":")
	fg.P("return ", m, ".NaN(), nil")
	fg.P("case \"Infinity\":")
	fg.P("return ", m, ".Inf(1), nil")
	fg.P("case \"-Infinity\":")
	fg.P("return ", m, ".Inf(-1), nil")
	fg.P("}")
	fg.P("}")
	fg.P("s, err := ", fg.fn("JSONNumber"), "(tok, name)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("v, err := ", fg.std("strconv"), ".ParseFloat(s, bits)")
	fg.P("if err != nil {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidNumber", "invalid number ", "s"))
	fg.P("}")
	fg.P("return v, nil")
	fg.P("}")
	fg.P()
}

func genJSONNumber(fg *fileGen, name string) {
	jt := fg.std("encoding/json/jsontext")
	fg.P("// ", name, " returns the text of tok, which must be a number or a string")
	fg.P("// holding a JSON number, for a field of message name.")
	fg.P("func ", name, "(tok ", jt, ".Token, name string) (string, error) {")
	fg.P("s := tok.String()")
	fg.P("if k := tok.Kind(); k != ", fg.kind("Number"), " && (k != ", fg.kind("String"), " || s == \"\" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) || !", jt, ".Value(s).IsValid()) {")
	fg.P("return \"\", ", fg.jsonErrorf("name", "ErrInvalidNumber", "invalid number ", "s"))
	fg.P("}")
	fg.P("return s, nil")
	fg.P("}")
	fg.P()
}

func genParseBool(fg *fileGen, name string) {
	fg.P("// ", name, " parses tok as a boolean for a field of message name.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, name string) (bool, error) {")
	fg.P("switch tok.Kind() {")
	fg.P("case ", fg.kind("True"), ":")
	fg.P("return true, nil")
	fg.P("case ", fg.kind("False"), ":")
	fg.P("return false, nil")
	fg.P("}")
	fg.P("return false, ", fg.jsonErrorf("name", "ErrInvalidBool", "invalid boolean ", "tok.String()"))
	fg.P("}")
	fg.P()
}

func genParseString(fg *fileGen, name string) {
	fg.P("// ", name, " parses tok as a string for a field of message name.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, name string) (string, error) {")
	fg.P("if tok.Kind() != ", fg.kind("String"), " {")
	fg.P("return \"\", ", fg.jsonErrorf("name", "ErrInvalidString", "invalid string ", "tok.String()"))
	fg.P("}")
	fg.P("return tok.String(), nil")
	fg.P("}")
	fg.P()
}

func genParseBytes(fg *fileGen, name string) {
	b64 := fg.std("encoding/base64")
	fg.P("// ", name, " parses tok as base64 bytes for a field of message name,")
	fg.P("// accepting standard and URL-safe alphabets, with or without padding.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, name string) ([]byte, error) {")
	fg.P("s := tok.String()")
	fg.P("if tok.Kind() == ", fg.kind("String"), " {")
	fg.P("enc := ", b64, ".StdEncoding")
	fg.P("if ", fg.std("strings"), ".ContainsAny(s, \"-_\") {")
	fg.P("enc = ", b64, ".URLEncoding")
	fg.P("}")
	fg.P("if len(s)%", fg.c("Base64Quantum"), " != 0 {")
	fg.P("enc = enc.WithPadding(", b64, ".NoPadding)")
	fg.P("}")
	fg.P("if by, err := enc.DecodeString(s); err == nil {")
	fg.P("return by, nil")
	fg.P("}")
	fg.P("}")
	fg.P("return nil, ", fg.jsonErrorf("name", "ErrInvalidBytes", "invalid bytes ", "s"))
	fg.P("}")
	fg.P()
}

func genParseHex(fg *fileGen, name string) {
	fg.P("// ", name, " parses tok as hex bytes, in either case, for a field of")
	fg.P("// message name.")
	fg.P("func ", name, "(tok ", fg.std("encoding/json/jsontext"), ".Token, name string) ([]byte, error) {")
	fg.P("s := tok.String()")
	fg.P("if tok.Kind() == ", fg.kind("String"), " {")
	fg.P("if by, err := ", fg.std("encoding/hex"), ".DecodeString(s); err == nil {")
	fg.P("return by, nil")
	fg.P("}")
	fg.P("}")
	fg.P("return nil, ", fg.jsonErrorf("name", "ErrInvalidHex", "invalid hex bytes ", "s"))
	fg.P("}")
	fg.P()
}

func genParseEnum(fg *fileGen, name string) {
	fg.P("// ", name, " converts an enum value read with the given class, a name in")
	fg.P("// tok or a number in iv, to E. values maps names to numbers.")
	fg.P("func ", name, "[E ~int32](class int, tok ", fg.std("encoding/json/jsontext"), ".Token, iv int64, values map[string]int32, name, enum string) (E, error) {")
	fg.P("switch class {")
	fg.P("case ", fg.class(classEnum), ":")
	fg.P("s := tok.String()")
	fg.P("n, ok := values[s]")
	fg.P("if !ok {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidEnum", "invalid value for enum ", "enum", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P("return E(n), nil")
	fg.P("case ", fg.class(classSigned), ":")
	fg.P("return E(iv), nil")
	fg.P("}")
	fg.P("return 0, nil")
	fg.P("}")
	fg.P()
}

func genParseIntKey(fg *fileGen, name string) {
	fg.P("// ", name, " parses JSON object key s as a signed integer of the given bit")
	fg.P("// size, for map field field of message name.")
	fg.P("func ", name, "(s string, bits int, name, field string) (int64, error) {")
	fg.P("k, err := ", fg.std("strconv"), ".ParseInt(s, 10, bits)")
	fg.P("if err != nil {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidKey", "invalid map key for field ", "field", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P("return k, nil")
	fg.P("}")
	fg.P()
}

func genParseUintKey(fg *fileGen, name string) {
	fg.P("// ", name, " parses JSON object key s as an unsigned integer of the given")
	fg.P("// bit size, for map field field of message name.")
	fg.P("func ", name, "(s string, bits int, name, field string) (uint64, error) {")
	fg.P("k, err := ", fg.std("strconv"), ".ParseUint(s, 10, bits)")
	fg.P("if err != nil {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidKey", "invalid map key for field ", "field", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P("return k, nil")
	fg.P("}")
	fg.P()
}

func genParseBoolKey(fg *fileGen, name string) {
	fg.P("// ", name, " parses JSON object key s as a boolean, for map field field of")
	fg.P("// message name.")
	fg.P("func ", name, "(s, name, field string) (bool, error) {")
	fg.P("switch s {")
	fg.P("case \"true\":")
	fg.P("return true, nil")
	fg.P("case \"false\":")
	fg.P("return false, nil")
	fg.P("}")
	fg.P("return false, ", fg.jsonErrorf("name", "ErrInvalidKey", "invalid map key for field ", "field", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P()
}

func genJSONError(fg *fileGen, name string) {
	fg.P("// ", name, " returns an error about the ProtoJSON value of message name.")
	fg.P("func ", name, "(name, msg string) error {")
	fg.P("return ", fg.std("errors"), ".New(\"proto: \" + name + \": \" + msg)")
	fg.P("}")
	fg.P()
}
