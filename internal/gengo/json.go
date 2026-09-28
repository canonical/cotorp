// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"encoding/json/jsontext"
	"sort"
	"strconv"

	"github.com/canonical/cotorp/internal/desc"
)

// This file generates ProtoJSON (https://protobuf.dev/programming-guides/json/)
// methods. As with the binary methods, only the standard library, the file's
// helper functions (see helpers.go) and other generated messages' methods are
// called.
//
// Marshaling appends to a byte slice (ProtoAppendJSON), using jsontext to
// quote strings and format floats. MarshalJSONTo hands that output to a
// jsontext.Encoder for encoding/json/v2.
//
// Unmarshaling reads tokens from a single jsontext.Decoder
// (ProtoMergeJSONFrom), and nested messages read from the same decoder. The
// decoder rejects malformed JSON, invalid UTF-8 and duplicate names. Each
// iteration of a message's loop reads one value: either a field's value or
// an element of the repeated or map field it is inside. Scalars are parsed by
// one helper call per class (so the parsing code appears once per file) and
// then assigned to the field. A token is only valid until the next read from the decoder, so each
// value is converted before the decoder is used again.

// Scalar parse classes.
const (
	classNone     = 0
	classSigned   = 1
	classUnsigned = 2
	classFloat    = 3
	classBool     = 4
	classString   = 5
	classBytes    = 6
	classHex      = 7 // bytes fields selected with Options.JSONHex
	classEnum     = 8 // an enum name, or resolved to classSigned or classNone by token kind
)

// jsonQuote returns the JSON encoding of s as a Go string literal.
func jsonQuote(s string) string {
	b, _ := jsontext.AppendQuote(nil, s)
	return string(b)
}

// acceptsNull reports whether JSON null is a value (rather than "unset") for
// fields of this type.
func acceptsNull(f *desc.Field) bool {
	switch {
	case f.Kind == desc.KindMessage && f.MessageType.FullName == "google.protobuf.Value":
		return true
	case f.Kind == desc.KindEnum && f.EnumType.FullName == "google.protobuf.NullValue":
		return true
	}
	return false
}

// errConcat returns an expression constructing an error whose message is
// the constant msg followed by the Go string expression parts. hint names
// msg's constant, as in errExpr.
func (fg *fileGen) errConcat(hint, msg string, parts ...string) string {
	e := fg.std("errors") + ".New(" + fg.errString(hint, msg)
	for _, p := range parts {
		e += " + " + p
	}
	return e + ")"
}

func (fg *fileGen) jsonErr(hint, msg string) string {
	return fg.errExpr(hint, "proto: "+msg)
}

// classConst names the constant for a parse class.
var classConst = [...]string{
	classNone:     "ClassNone",
	classSigned:   "ClassSigned",
	classUnsigned: "ClassUnsigned",
	classFloat:    "ClassFloat",
	classBool:     "ClassBool",
	classString:   "ClassString",
	classBytes:    "ClassBytes",
	classHex:      "ClassHex",
	classEnum:     "ClassEnum",
}

// class returns the constant naming parse class c.
func (fg *fileGen) class(c int) string {
	return fg.c(classConst[c])
}

// kind returns the jsontext constant for a token kind, such as "Null" or
// "BeginObject".
func (fg *fileGen) kind(name string) string {
	return fg.std("encoding/json/jsontext") + ".Kind" + name
}

// --- Marshal ----------------------------------------------------------------

// appendLit returns an expression appending the literal text lit to the
// byte slice expression dst.
func appendLit(dst, lit string) string {
	switch len(lit) {
	case 0:
		return dst
	case 1:
		return "append(" + dst + ", " + strconv.QuoteRune(rune(lit[0])) + ")"
	}
	return "append(" + dst + ", " + strconv.Quote(lit) + "...)"
}

// jsonOutExpr returns an expression appending lit and then the JSON form of
// singular value v of field f to the byte slice expression dst. It reports
// false for strings and messages, which can fail and need statements (see
// jsonOut).
func (fg *fileGen) jsonOutExpr(f *desc.Field, v, dst, lit string) (string, bool) {
	conv := fg.std("strconv")
	d := appendLit(dst, lit)
	quoted := func(e string) string { return "append(" + e + ", '\"')" }
	switch f.Kind {
	case desc.KindInt32, desc.KindSint32, desc.KindSfixed32:
		return conv + ".AppendInt(" + d + ", int64(" + v + "), 10)", true
	case desc.KindUint32, desc.KindFixed32:
		return conv + ".AppendUint(" + d + ", uint64(" + v + "), 10)", true
	case desc.KindInt64, desc.KindSint64, desc.KindSfixed64:
		return quoted(conv + ".AppendInt(" + appendLit(dst, lit+`"`) + ", " + v + ", 10)"), true
	case desc.KindUint64, desc.KindFixed64:
		return quoted(conv + ".AppendUint(" + appendLit(dst, lit+`"`) + ", " + v + ", 10)"), true
	case desc.KindBool:
		return conv + ".AppendBool(" + d + ", " + v + ")", true
	case desc.KindFloat:
		return fg.fn("AppendFloat") + "(" + d + ", float64(" + v + "), 32)", true
	case desc.KindDouble:
		return fg.fn("AppendFloat") + "(" + d + ", " + v + ", 64)", true
	case desc.KindBytes:
		enc := fg.std("encoding/base64") + ".StdEncoding"
		if fg.g.jsonHex[f.FullName] {
			enc = fg.std("encoding/hex")
		}
		return quoted(enc + ".AppendEncode(" + appendLit(dst, lit+`"`) + ", " + v + ")"), true
	case desc.KindEnum:
		switch {
		case f.EnumType.FullName == "google.protobuf.NullValue":
			return appendLit(dst, lit+"null"), true
		case fg.g.opts.JSONEnumNumbers:
			return conv + ".AppendInt(" + d + ", int64(" + v + "), 10)", true
		}
		return fg.fn("AppendEnum") + "(" + d + ", int32(" + v + "), " + fg.qualify(f.EnumType.File, fg.g.enmNames[f.EnumType]+"_name") + ")", true
	}
	return "", false
}

// jsonOut emits code appending lit and then the JSON form of singular value
// v of field f to b, followed by a comma if comma is set.
func (fg *fileGen) jsonOut(f *desc.Field, v, lit string, comma bool) {
	if e, ok := fg.jsonOutExpr(f, v, "b", lit); ok {
		if comma {
			e = "append(" + e + ", ',')"
		}
		fg.P("b = ", e)
		return
	}
	fg.usesErr = true
	if f.Kind == desc.KindString {
		fg.jsonAppendString(appendLit("b", lit), v, f.FullName)
	} else {
		fg.P("if b, err = ", v, ".ProtoAppendJSON(", appendLit("b", lit), "); err != nil {")
		fg.P("return nil, err")
		fg.P("}")
	}
	if comma {
		fg.P("b = append(b, ',')")
	}
}

// jsonAppendString emits code appending string v to the byte slice
// expression dst as a JSON string, assigning the result to b. name is used
// in the invalid UTF-8 error.
func (fg *fileGen) jsonAppendString(dst, v, name string) {
	fg.usesErr = true
	fg.P("if b, err = ", fg.std("encoding/json/jsontext"), ".AppendQuote(", dst, ", ", v, "); err != nil {")
	fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrJSONUTF8", name+" contains invalid UTF-8"))
	fg.P("}")
}

// jsonMapEntries emits `key:value,` for each entry of map fv, in key order.
func (fg *fileGen) jsonMapEntries(f *desc.Field, fv string) {
	entry := func() {
		conv := fg.std("strconv")
		// Non-string keys are quoted by appending the closing quote with
		// the colon.
		sep := `":`
		switch f.MapKey.Kind {
		case desc.KindString:
			fg.jsonAppendString("b", "k", f.FullName)
			sep = ":"
		case desc.KindBool:
			fg.P("b = ", conv, ".AppendBool(append(b, '\"'), k)")
		case desc.KindUint32, desc.KindFixed32, desc.KindUint64, desc.KindFixed64:
			fg.P("b = ", conv, ".AppendUint(append(b, '\"'), uint64(k), 10)")
		default:
			fg.P("b = ", conv, ".AppendInt(append(b, '\"'), int64(k), 10)")
		}
		fg.jsonOut(f.MapValue, "v", sep, true)
	}
	if f.MapKey.Kind == desc.KindBool {
		fg.P("for _, k := range [2]bool{false, true} {")
		fg.P("v, ok := ", fv, "[k]")
		fg.P("if !ok {")
		fg.P("continue")
		fg.P("}")
		entry()
		fg.P("}")
		return
	}
	fg.P("for _, k := range ", fg.fn("SortedKeys"), "(", fv, ", make([]", fg.scalarGoType(f.MapKey), ", 0, len(", fv, "))) {")
	fg.P("v := ", fv, "[k]")
	entry()
	fg.P("}")
}

// jsonAppendField emits code appending `,"name":value` for a populated
// non-oneof field.
func (fg *fileGen) jsonAppendField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	key := "," + jsonQuote(f.JSONName) + ":"
	switch {
	case f.IsMap:
		fg.P("if len(", fv, ") > 0 {")
		fg.P("b = ", appendLit("b", key+"{"))
		fg.jsonMapEntries(f, fv)
		fg.P("b[len(b)-1] = '}'")
		fg.P("}")
	case f.Repeated:
		fg.P("if len(", fv, ") > 0 {")
		fg.P("b = ", appendLit("b", key+"["))
		fg.P("for j := range ", fv, " {")
		fg.jsonOut(f, fv+"[j]", "", true)
		fg.P("}")
		fg.P("b[len(b)-1] = ']'")
		fg.P("}")
	case f.Kind == desc.KindMessage, f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.jsonOut(f, fv, key, false)
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.jsonOut(f, "(*"+fv+")", key, false)
		fg.P("}")
	default:
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.jsonOut(f, fv, key, false)
		fg.P("}")
	}
}

func (fg *fileGen) genJSONMarshal(mi *messageInfo, body func()) {
	name := mi.goName
	checkInit := func(ret string) {
		if mi.m.HasRequired() {
			fg.P("if err := m.ProtoCheckInitialized(); err != nil {")
			fg.P("return ", ret)
			fg.P("}")
		}
	}
	fg.P("// MarshalJSON returns the ProtoJSON encoding of m.")
	if mi.m.HasRequired() {
		fg.P("func (m *", name, ") MarshalJSON() ([]byte, error) {")
		checkInit("nil, err")
		fg.P("return m.ProtoAppendJSON(nil)")
		fg.P("}")
	} else {
		fg.P("func (m *", name, ") MarshalJSON() ([]byte, error) { return m.ProtoAppendJSON(nil) }")
	}
	fg.P()
	fg.P("// MarshalJSONTo writes the ProtoJSON encoding of m to e. It implements")
	fg.P("// json.MarshalerTo from encoding/json/v2.")
	fg.P("func (m *", name, ") MarshalJSONTo(e *", fg.std("encoding/json/jsontext"), ".Encoder) error {")
	checkInit("err")
	fg.P("b, err := m.ProtoAppendJSON(e.AvailableBuffer())")
	fg.P("return ", fg.fn("WriteJSON"), "(e, b, err)")
	fg.P("}")
	fg.P()
	fg.usesErr = false
	code := fg.capture(body)
	fg.P("// ProtoAppendJSON appends the ProtoJSON encoding of m to b. It does not")
	fg.P("// check required fields.")
	fg.P("func (m *", name, ") ProtoAppendJSON(b []byte) ([]byte, error) {")
	if fg.usesErr {
		fg.P("var err error")
	}
	fg.buf.WriteString(code)
	fg.P("}")
	fg.P()
}

// genericJSONMarshalBody emits the body of ProtoAppendJSON for an ordinary
// message. Each member is appended with a leading comma, and CloseObject
// turns the first one into the opening brace.
func (fg *fileGen) genericJSONMarshalBody(mi *messageInfo) {
	fg.P("if m == nil {")
	fg.P("return append(b, \"{}\"...), nil")
	fg.P("}")
	fg.P("start := len(b)")
	for _, fi := range mi.fields {
		if fi.oneof != nil {
			v := "o"
			if fi.f.Kind == desc.KindEnum && fi.f.EnumType.FullName == "google.protobuf.NullValue" {
				v = "_" // always written as null
			}
			fg.P("if ", v, ", ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
			fg.jsonOut(fi.f, "o."+fi.goName, ","+jsonQuote(fi.f.JSONName)+":", false)
			fg.P("}")
			continue
		}
		fg.jsonAppendField(fi)
	}
	fg.P("return ", fg.fn("CloseObject"), "(b, start), nil")
}

// --- Unmarshal --------------------------------------------------------------

func (fg *fileGen) genJSONUnmarshal(mi *messageInfo, body func()) {
	name := mi.goName
	full := mi.m.FullName
	jt := fg.std("encoding/json/jsontext")
	merge := func(call string) {
		if mi.m.HasRequired() {
			fg.P("if err := m.", call, "; err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("return m.ProtoCheckInitialized()")
		} else {
			fg.P("return m.", call)
		}
	}
	fg.P("// UnmarshalJSON replaces the contents of m with the decoded ProtoJSON")
	fg.P("// value in b.")
	fg.P("func (m *", name, ") UnmarshalJSON(b []byte) error {")
	fg.P("*m = ", name, "{}")
	merge("ProtoMergeJSON(b)")
	fg.P("}")
	fg.P()
	fg.P("// ProtoMergeJSON decodes the ProtoJSON value in b and merges it into m.")
	fg.P("// It does not check required fields.")
	fg.P("func (m *", name, ") ProtoMergeJSON(b []byte) error {")
	fg.P("d := ", jt, ".NewDecoder(", fg.std("bytes"), ".NewBuffer(b))")
	fg.P("return ", fg.fn("EndJSON"), "(d, m.ProtoMergeJSONFrom(d), ", strconv.Quote(full), ")")
	fg.P("}")
	fg.P()
	fg.P("// UnmarshalJSONFrom replaces the contents of m with the ProtoJSON value")
	fg.P("// read from d. It implements json.UnmarshalerFrom from encoding/json/v2.")
	fg.P("func (m *", name, ") UnmarshalJSONFrom(d *", jt, ".Decoder) error {")
	fg.P("d, err := ", fg.fn("StrictDecoder"), "(d)")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("*m = ", name, "{}")
	merge("ProtoMergeJSONFrom(d)")
	fg.P("}")
	fg.P()
	fg.P("// ProtoMergeJSONFrom decodes one ProtoJSON value from d and merges it")
	fg.P("// into m. It does not check required fields. d should reject invalid")
	fg.P("// UTF-8, as jsontext decoders do by default.")
	fg.P("func (m *", name, ") ProtoMergeJSONFrom(d *", jt, ".Decoder) error {")
	body()
	fg.P("}")
	fg.P()
}

// jsonReadOpen emits code reading the first token of a value, which must be
// of the given kind; null leaves m unchanged. It declares tok and err.
func (fg *fileGen) jsonReadOpen(name, kind, hint, what string) {
	fg.P("tok, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if tok.Kind() == ", fg.kind("Null"), " {")
	fg.P("// JSON null leaves the message unchanged.")
	fg.P("return nil")
	fg.P("}")
	fg.P("if tok.Kind() != ", fg.kind(kind), " {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+hint, name+": expected a JSON "+what))
	fg.P("}")
}

// jsonOpen emits code reading the first token of the value of message name,
// which must be of the given kind; null leaves m unchanged. It declares err.
func (fg *fileGen) jsonOpen(name, kind, what string) {
	fg.P("ok, err := ", fg.fn("OpenJSON"), "(d, ", fg.kind(kind), ", ", strconv.Quote(name), ", ", strconv.Quote(what), ")")
	fg.P("if !ok {")
	fg.P("return err")
	fg.P("}")
}

// jsonUnknown emits code rejecting unknown field key, or skipping its value
// with Options.JSONDiscardUnknown.
func (fg *fileGen) jsonUnknown(name, key string) {
	if fg.g.opts.JSONDiscardUnknown {
		fg.P("// Unknown keys are ignored (-json_discard_unknown).")
		fg.P("if err := d.SkipValue(); err != nil {")
		fg.P("return err")
		fg.P("}")
		return
	}
	fg.P("return ", fg.errConcat(fg.owner(name)+"ErrUnknownField", "proto: "+name+": unknown field ", fg.std("strconv")+".Quote("+key+")"))
}

// genericJSONUnmarshalBody emits the body of ProtoMergeJSONFrom for an
// ordinary message.
func (fg *fileGen) genericJSONUnmarshalBody(mi *messageInfo) {
	name := mi.m.FullName
	fg.jsonOpen(name, "BeginObject", "object")
	if len(mi.fields) == 0 {
		if fg.g.opts.JSONDiscardUnknown {
			fg.P("for d.PeekKind() != ", fg.kind("EndObject"), " {")
			fg.P("if _, err := d.ReadToken(); err != nil {")
			fg.P("return err")
			fg.P("}")
		} else {
			fg.P("if d.PeekKind() != ", fg.kind("EndObject"), " {")
			fg.P("kt, err := d.ReadToken()")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
		}
		fg.jsonUnknown(name, "kt.String()")
		fg.P("}")
		fg.P("_, err = d.ReadToken()")
		fg.P("return err")
		return
	}

	// Group fields by structural handling.
	var nullOK, repeated, repeatedNull, maps, mapsNull []int
	oneofMembers := map[int][]int{}
	oneofIdx := map[*oneofInfo]int{}
	for i, oi := range mi.oneofs {
		oneofIdx[oi] = i
	}
	for i, fi := range mi.fields {
		f := fi.f
		switch {
		case f.IsMap && acceptsNull(f.MapValue):
			mapsNull = append(mapsNull, i)
		case f.IsMap:
			maps = append(maps, i)
		case f.Repeated && acceptsNull(f):
			repeatedNull = append(repeatedNull, i)
		case f.Repeated:
			repeated = append(repeated, i)
		case acceptsNull(f):
			nullOK = append(nullOK, i)
		}
		if fi.oneof != nil {
			oneofMembers[oneofIdx[fi.oneof]] = append(oneofMembers[oneofIdx[fi.oneof]], i)
		}
	}
	arrays := append(append([]int(nil), repeated...), repeatedNull...)
	objects := append(append([]int(nil), maps...), mapsNull...)
	containers := len(arrays)+len(objects) > 0

	fg.P("var seen [", len(mi.fields), "]bool")
	if len(mi.oneofs) > 0 {
		fg.P("var oneofs [", len(mi.oneofs), "]bool")
	}
	if containers {
		fg.P("// in is the kind of the array or object of repeated or map field f")
		fg.P("// while its elements are read.")
		fg.P("var in ", fg.std("encoding/json/jsontext"), ".Kind")
	}
	if len(objects) > 0 {
		fg.P("var mk string")
	}
	fg.P("var f int")
	fg.P("for {")
	if containers {
		fg.P("if in == ", fg.kind("Invalid"), " {")
	}
	fg.P("if d.PeekKind() == ", fg.kind("EndObject"), " {")
	fg.P("break")
	fg.P("}")
	fg.P("kt, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("key := kt.String()")

	// Keys: JSON names take precedence over proto names, as in protojson.
	type keyIdx struct {
		key string
		idx int
	}
	var keys []keyIdx
	claimed := map[string]bool{}
	for i, fi := range mi.fields {
		if !claimed[fi.f.JSONName] {
			claimed[fi.f.JSONName] = true
			keys = append(keys, keyIdx{fi.f.JSONName, i})
		}
	}
	for i, fi := range mi.fields {
		if !claimed[fi.f.Name] {
			claimed[fi.f.Name] = true
			keys = append(keys, keyIdx{fi.f.Name, i})
		}
	}
	byIdx := map[int][]string{}
	for _, k := range keys {
		byIdx[k.idx] = append(byIdx[k.idx], strconv.Quote(k.key))
	}
	fg.P("switch key {")
	for i := range mi.fields {
		if ks := byIdx[i]; len(ks) > 0 {
			fg.P("case ", joinComma(ks), ":")
			fg.P("f = ", i)
		}
	}
	fg.P("default:")
	fg.jsonUnknown(name, "key")
	if fg.g.opts.JSONDiscardUnknown {
		fg.P("continue")
	}
	fg.P("}")
	fg.P("if seen[f] {")
	fg.P("return ", fg.errConcat(fg.owner(name)+"ErrDuplicateField", "proto: "+name+": duplicate field ", fg.std("strconv")+".Quote(key)"))
	fg.P("}")
	fg.P("seen[f] = true")
	fg.P("if d.PeekKind() == ", fg.kind("Null"), notIn("f", nullOK), " {")
	fg.P("// null leaves the field unset.")
	fg.P("if err := d.SkipValue(); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("continue")
	fg.P("}")
	if len(mi.oneofs) > 0 || containers {
		fg.P("switch f {")
		for k := range mi.oneofs {
			fg.P("case ", joinInts(oneofMembers[k]), ":")
			fg.P("if oneofs[", k, "] {")
			fg.P("return ", fg.jsonErr(fg.owner(name)+camelCase(mi.oneofs[k].o.Name)+"ErrOneofConflict", name+": multiple fields set for oneof "+mi.oneofs[k].o.Name))
			fg.P("}")
			fg.P("oneofs[", k, "] = true")
		}
		open := func(idxs []int, kind, what string) {
			if len(idxs) == 0 {
				return
			}
			fg.P("case ", joinInts(idxs), ":")
			fg.P("if err := ", fg.fn("ExpectJSON"), "(d, ", fg.kind(kind), ", ", strconv.Quote(name), ", ", strconv.Quote(what), "); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("in = ", fg.kind(kind))
			fg.P("continue")
		}
		open(arrays, "BeginArray", "array")
		open(objects, "BeginObject", "object")
		fg.P("}")
	}
	if containers {
		fg.P("} else {")
		fg.P("if k := d.PeekKind(); k == ", fg.kind("EndArray"), " || k == ", fg.kind("EndObject"), " {")
		fg.P("if _, err := d.ReadToken(); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("in = ", fg.kind("Invalid"))
		fg.P("continue")
		fg.P("}")
		if len(objects) > 0 {
			fg.P("if in == ", fg.kind("BeginObject"), " {")
			fg.P("kt, err := d.ReadToken()")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("mk = kt.String()")
			fg.P("}")
		}
		if len(repeated)+len(maps) > 0 {
			fg.P("if d.PeekKind() == ", fg.kind("Null"), notIn("f", append(append([]int(nil), repeatedNull...), mapsNull...)), " {")
			fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrNullElement", name+": null is not allowed in repeated fields or map values"))
			fg.P("}")
		}
		fg.P("}")
	}
	fg.jsonValue(mi, allIndexes(mi))
	fg.P("}")
	fg.P("_, err = d.ReadToken()")
	fg.P("return err")
}

func allIndexes(mi *messageInfo) []int {
	idx := make([]int, len(mi.fields))
	for i := range idx {
		idx[i] = i
	}
	return idx
}

func joinInts(xs []int) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = strconv.Itoa(x)
	}
	return joinComma(s)
}

func joinComma(xs []string) string {
	s := ""
	for i, x := range xs {
		if i > 0 {
			s += ", "
		}
		s += x
	}
	return s
}

// notIn returns " && v != x" for each of xs.
func notIn(v string, xs []int) string {
	s := ""
	for _, x := range xs {
		s += " && " + v + " != " + strconv.Itoa(x)
	}
	return s
}

// fieldClass is scalarClass, except that bytes fields selected with
// Options.JSONHex parse as hex.
func (fg *fileGen) fieldClass(f *desc.Field) (int, int) {
	if f.Kind == desc.KindBytes && fg.g.jsonHex[f.FullName] {
		return classHex, 0
	}
	return scalarClass(f.Kind)
}

// scalarClass returns the parse class and bit size for a non-message,
// non-enum kind.
func scalarClass(k desc.Kind) (int, int) {
	switch k {
	case desc.KindInt32, desc.KindSint32, desc.KindSfixed32:
		return classSigned, 32
	case desc.KindInt64, desc.KindSint64, desc.KindSfixed64:
		return classSigned, 64
	case desc.KindUint32, desc.KindFixed32:
		return classUnsigned, 32
	case desc.KindUint64, desc.KindFixed64:
		return classUnsigned, 64
	case desc.KindFloat:
		return classFloat, 32
	case desc.KindDouble:
		return classFloat, 64
	case desc.KindBool:
		return classBool, 0
	case desc.KindString:
		return classString, 0
	case desc.KindBytes:
		return classBytes, 0
	}
	return classNone, 0
}

// valueField returns the field whose values fi's JSON values carry: the map
// value for maps, otherwise the field itself.
func valueField(fi *fieldInfo) *desc.Field {
	if fi.f.IsMap {
		return fi.f.MapValue
	}
	return fi.f
}

// jsonValue emits code that reads one value for field f, which is one of
// idxs, and assigns it. With a single index, f is not used.
func (fg *fileGen) jsonValue(mi *messageInfo, idxs []int) {
	name := mi.m.FullName
	single := len(idxs) == 1
	used := map[int]bool{}
	groups := map[[2]int][]int{}
	for _, i := range idxs {
		vf := valueField(mi.fields[i])
		k := [2]int{classEnum, 0}
		switch vf.Kind {
		case desc.KindMessage:
			continue
		case desc.KindEnum:
			used[classSigned] = true
		default:
			k[0], k[1] = fg.fieldClass(vf)
			used[k[0]] = true
		}
		groups[k] = append(groups[k], i)
	}
	if len(used) > 0 {
		numeric := used[classSigned] || used[classUnsigned] || used[classFloat]
		gkeys := make([][2]int, 0, len(groups))
		for k := range groups {
			gkeys = append(gkeys, k)
		}
		sort.Slice(gkeys, func(a, b int) bool {
			if gkeys[a][0] != gkeys[b][0] {
				return gkeys[a][0] < gkeys[b][0]
			}
			return gkeys[a][1] < gkeys[b][1]
		})
		_, hasEnum := groups[[2]int{classEnum, 0}]
		// The class, bit size and scratch variables are declared together.
		var names, values []string
		decl := func(name, value string) {
			names = append(names, name)
			values = append(values, value)
		}
		if single {
			k := gkeys[0]
			decl("class", fg.class(k[0]))
			if numeric {
				if k[1] == 0 {
					k[1] = 64
				}
				decl("bits", strconv.Itoa(k[1]))
			}
		} else {
			decl("class", fg.class(classNone))
			if numeric {
				decl("bits", "64")
			}
		}
		scratch := map[int][2]string{classSigned: {"iv", "int64(0)"}, classUnsigned: {"uv", "uint64(0)"}, classFloat: {"fv", "float64(0)"}, classBool: {"bv", "false"}, classString: {"sv", `""`}}
		for _, c := range []int{classSigned, classUnsigned, classFloat, classBool, classString} {
			if used[c] {
				decl(scratch[c][0], scratch[c][1])
			}
		}
		if used[classBytes] || used[classHex] {
			decl("by", "[]byte(nil)")
		}
		if !single {
			decl("tok", fg.std("encoding/json/jsontext")+".Token{}")
		}
		fg.P(joinComma(names), " := ", joinComma(values))
		if !single {
			// Select the parse class for the field.
			fg.P("switch f {")
			for _, k := range gkeys {
				fg.P("case ", joinInts(groups[k]), ":")
				if numeric && k[1] != 0 {
					fg.P("class, bits = ", fg.class(k[0]), ", ", k[1])
				} else {
					fg.P("class = ", fg.class(k[0]))
				}
			}
			fg.P("}")
		}
		if single {
			fg.P("tok, err := d.ReadToken()")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
		} else {
			// err is declared by jsonOpen or by the key read.
			fg.P("if class != ", fg.class(classNone), " {")
			fg.P("if tok, err = d.ReadToken(); err != nil {")
			fg.P("return err")
			fg.P("}")
		}
		if hasEnum {
			fg.P("if class == ", fg.class(classEnum), " {")
			fg.P("class, bits = ", fg.fn("EnumClass"), "(tok.Kind())")
			fg.P("}")
		}
		if !single {
			fg.P("}")
		}
		fg.jsonParseClasses(used, name)
	}
	if single {
		fg.jsonAssign(mi.fields[idxs[0]])
		return
	}
	fg.P("switch f {")
	for _, i := range idxs {
		fg.P("case ", i, ":")
		fg.jsonAssign(mi.fields[i])
	}
	fg.P("}")
}

// jsonParseClasses emits a switch converting token tok to the scratch
// variable for its class, using the parse helpers.
func (fg *fileGen) jsonParseClasses(used map[int]bool, name string) {
	q := strconv.Quote(name)
	fg.P("switch class {")
	for _, c := range []struct {
		class       int
		dst, helper string
		bits        bool
	}{
		{classSigned, "iv", "ParseInt", true},
		{classUnsigned, "uv", "ParseUint", true},
		{classFloat, "fv", "ParseFloat", true},
		{classBool, "bv", "ParseBool", false},
		{classString, "sv", "ParseString", false},
		{classBytes, "by", "ParseBytes", false},
		{classHex, "by", "ParseHex", false},
	} {
		if !used[c.class] {
			continue
		}
		fg.P("case ", fg.class(c.class), ":")
		if c.bits {
			fg.P(c.dst, ", err = ", fg.fn(c.helper), "(tok, bits, ", q, ")")
		} else {
			fg.P(c.dst, ", err = ", fg.fn(c.helper), "(tok, ", q, ")")
		}
	}
	fg.P("}")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
}

// scratchExpr converts scratch variables to f's Go type.
func (fg *fileGen) scratchExpr(f *desc.Field) string {
	switch f.Kind {
	case desc.KindInt32, desc.KindSint32, desc.KindSfixed32:
		return "int32(iv)"
	case desc.KindInt64, desc.KindSint64, desc.KindSfixed64:
		return "iv"
	case desc.KindUint32, desc.KindFixed32:
		return "uint32(uv)"
	case desc.KindUint64, desc.KindFixed64:
		return "uv"
	case desc.KindFloat:
		return "float32(fv)"
	case desc.KindDouble:
		return "fv"
	case desc.KindBool:
		return "bv"
	case desc.KindString:
		return "sv"
	case desc.KindBytes:
		return "by"
	}
	panic("scratchExpr: " + f.Kind.String())
}

// jsonValueExpr emits any statements needed to produce the value for value
// field vf and returns an expression for it.
func (fg *fileGen) jsonValueExpr(vf *desc.Field, name string) string {
	switch vf.Kind {
	case desc.KindEnum:
		values := fg.qualify(vf.EnumType.File, fg.g.enmNames[vf.EnumType]+"_value")
		fg.P("ev, err := ", fg.fn("ParseEnum"), "[", fg.enumType(vf.EnumType), "](class, tok, iv, ", values, ", ", strconv.Quote(name), ", ", strconv.Quote(vf.EnumType.FullName), ")")
		fg.P("if err != nil {")
		fg.P("return err")
		fg.P("}")
		return "ev"
	case desc.KindMessage:
		fg.P("mv := &", fg.msgType(vf.MessageType), "{}")
		fg.P("if err := mv.ProtoMergeJSONFrom(d); err != nil {")
		fg.P("return err")
		fg.P("}")
		return "mv"
	}
	return fg.scratchExpr(vf)
}

// jsonAssign emits the assignment of the value just read to one field.
func (fg *fileGen) jsonAssign(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	name := f.Parent.FullName
	switch {
	case f.IsMap:
		key := f.MapKey
		k := "mk"
		args := "(mk, " + strconv.Quote(name) + ", " + strconv.Quote(f.Name) + ")"
		switch key.Kind {
		case desc.KindString:
		case desc.KindBool:
			fg.P("k, err := ", fg.fn("ParseBoolKey"), args)
			k = "k"
		default:
			c, bits := scalarClass(key.Kind)
			parse := "ParseIntKey"
			if c == classUnsigned {
				parse = "ParseUintKey"
			}
			fg.P("k, err := ", fg.fn(parse), "(mk, ", bits, ", ", strconv.Quote(name), ", ", strconv.Quote(f.Name), ")")
			k = "k"
			if bits == 32 {
				k = fg.scalarGoType(key) + "(k)"
			}
		}
		if k != "mk" {
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
		}
		val := fg.jsonValueExpr(f.MapValue, name)
		fg.P(fg.fn("MapSet"), "(&", fv, ", ", k, ", ", val, ")")
	case fi.oneof != nil:
		val := fg.jsonValueExpr(f, name)
		fg.P("m.", fi.oneof.goName, " = &", fi.wrapper, "{", fi.goName, ": ", val, "}")
	case f.Repeated:
		val := fg.jsonValueExpr(f, name)
		fg.P(fv, " = append(", fv, ", ", val, ")")
	case f.Kind == desc.KindMessage:
		fg.P("if ", fv, " == nil {")
		fg.P(fv, " = &", fg.msgType(f.MessageType), "{}")
		fg.P("}")
		fg.P("if err := ", fv, ".ProtoMergeJSONFrom(d); err != nil {")
		fg.P("return err")
		fg.P("}")
	case f.HasPresence && f.Kind != desc.KindBytes:
		fg.P(fv, " = new(", fg.jsonValueExpr(f, name), ")")
	default:
		val := fg.jsonValueExpr(f, name)
		fg.P(fv, " = ", val)
	}
}

// --- Entry point and well-known types -----------------------------------------

func (fg *fileGen) genJSON(mi *messageInfo) {
	switch mi.m.FullName {
	case "google.protobuf.Timestamp":
		fg.genJSONTimestamp(mi)
	case "google.protobuf.Duration":
		fg.genJSONDuration(mi)
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue",
		"google.protobuf.Int64Value", "google.protobuf.UInt64Value",
		"google.protobuf.Int32Value", "google.protobuf.UInt32Value",
		"google.protobuf.BoolValue", "google.protobuf.StringValue",
		"google.protobuf.BytesValue":
		fg.genJSONWrapper(mi)
	case "google.protobuf.Struct":
		fg.genJSONContainer(mi, "fields", true)
	case "google.protobuf.ListValue":
		fg.genJSONContainer(mi, "values", false)
	case "google.protobuf.Value":
		fg.genJSONValue(mi)
	case "google.protobuf.FieldMask":
		fg.genJSONFieldMask(mi)
	case "google.protobuf.Any":
		fg.genJSONAny(mi)
	default:
		fg.genJSONMarshal(mi, func() { fg.genericJSONMarshalBody(mi) })
		fg.genJSONUnmarshal(mi, func() { fg.genericJSONUnmarshalBody(mi) })
	}
}

func fieldByName(mi *messageInfo, name string) (*fieldInfo, int) {
	for i, fi := range mi.fields {
		if fi.f.Name == name {
			return fi, i
		}
	}
	panic("well-known type " + mi.m.FullName + " has no field " + name)
}

// jsonReadString emits code reading a JSON string into s; null leaves m
// unchanged.
func (fg *fileGen) jsonReadString(name string) {
	fg.jsonReadOpen(name, "String", "ErrNotString", "string")
	fg.P("s := tok.String()")
}

// jsonAppendFraction emits code appending ".ddd", ".dddddd" or ".ddddddddd"
// for non-negative nanosecond count expression nanos, or nothing if zero.
// Each form adds one second in its unit before formatting and drops the
// leading "1", which leaves the zero-padded digits.
func (fg *fileGen) jsonAppendFraction(nanos string) {
	conv := fg.std("strconv")
	perMilli, perMicro := fg.c("NanosPerMilli"), fg.c("NanosPerMicro")
	fg.P("switch {")
	fg.P("case ", nanos, " == 0:")
	fg.P("case ", nanos, "%", perMilli, " == 0:")
	fg.P("b = append(b, '.')")
	fg.P("b = append(b, ", conv, ".Itoa(", fg.c("MillisPerSecond"), "+int(", nanos, ")/", perMilli, ")[1:]...)")
	fg.P("case ", nanos, "%", perMicro, " == 0:")
	fg.P("b = append(b, '.')")
	fg.P("b = append(b, ", conv, ".Itoa(", fg.c("MicrosPerSecond"), "+int(", nanos, ")/", perMicro, ")[1:]...)")
	fg.P("default:")
	fg.P("b = append(b, '.')")
	fg.P("b = append(b, ", conv, ".Itoa(", fg.c("NanosPerSecond"), "+int(", nanos, "))[1:]...)")
	fg.P("}")
}

func (fg *fileGen) genJSONTimestamp(mi *messageInfo) {
	name := mi.m.FullName
	sec, _ := fieldByName(mi, "seconds")
	nan, _ := fieldByName(mi, "nanos")
	S, N := "m."+sec.goName, "m."+nan.goName
	fg.genJSONMarshal(mi, func() {
		fg.P("var s int64")
		fg.P("var ns int32")
		fg.P("if m != nil {")
		fg.P("s, ns = ", S, ", ", N)
		fg.P("}")
		fg.P("if s < ", fg.c("MinTimestampSeconds"), " || s > ", fg.c("MaxTimestampSeconds"), " || ns < 0 || ns >= ", fg.c("NanosPerSecond"), " {")
		fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrOutOfRange", name+": timestamp out of range"))
		fg.P("}")
		fg.P("b = append(b, '\"')")
		fg.P("b = ", fg.std("time"), ".Unix(s, 0).UTC().AppendFormat(b, \"2006-01-02T15:04:05\")")
		fg.jsonAppendFraction("ns")
		fg.P("b = append(b, \"Z\\\"\"...)")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.jsonReadString(name)
		fg.P("t, err := ", fg.std("time"), ".Parse(", fg.std("time"), ".RFC3339Nano, s)")
		fg.P("if err != nil {")
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrInvalid", "proto: "+name+": invalid timestamp ", fg.std("strconv")+".Quote(s)"))
		fg.P("}")
		fg.P("secs := t.Unix()")
		fg.P("if secs < ", fg.c("MinTimestampSeconds"), " || secs > ", fg.c("MaxTimestampSeconds"), " {")
		fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrOutOfRange", name+": timestamp out of range"))
		fg.P("}")
		fg.P(S, ", ", N, " = secs, int32(t.Nanosecond())")
		fg.P("return nil")
	})
}

func (fg *fileGen) genJSONDuration(mi *messageInfo) {
	name := mi.m.FullName
	sec, _ := fieldByName(mi, "seconds")
	nan, _ := fieldByName(mi, "nanos")
	S, N := "m."+sec.goName, "m."+nan.goName
	conv := fg.std("strconv")
	fg.genJSONMarshal(mi, func() {
		fg.P("var s int64")
		fg.P("var ns int32")
		fg.P("if m != nil {")
		fg.P("s, ns = ", S, ", ", N)
		fg.P("}")
		maxSecs, nps := fg.c("MaxDurationSeconds"), fg.c("NanosPerSecond")
		fg.P("if s < -", maxSecs, " || s > ", maxSecs, " || ns <= -", nps, " || ns >= ", nps, " || (s > 0 && ns < 0) || (s < 0 && ns > 0) {")
		fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrOutOfRange", name+": duration out of range"))
		fg.P("}")
		fg.P("b = append(b, '\"')")
		fg.P("if s < 0 || ns < 0 {")
		fg.P("b = append(b, '-')")
		fg.P("s, ns = -s, -ns")
		fg.P("}")
		fg.P("b = ", conv, ".AppendInt(b, s, 10)")
		fg.jsonAppendFraction("ns")
		fg.P("b = append(b, \"s\\\"\"...)")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.jsonReadString(name)
		str := fg.std("strings")
		fg.P("in := s")
		fg.P("neg := false")
		fg.P("if ", str, ".HasPrefix(s, \"-\") {")
		fg.P("neg, s = true, s[1:]")
		fg.P("}")
		fg.P("whole, frac, dot := ", str, ".Cut(", str, ".TrimSuffix(s, \"s\"), \".\")")
		fg.P("if !", str, ".HasSuffix(s, \"s\") || whole == \"\" || ", str, ".Trim(whole, \"0123456789\") != \"\" ||")
		fg.P("(dot && (frac == \"\" || len(frac) > ", fg.c("MaxFracDigits"), " || ", str, ".Trim(frac, \"0123456789\") != \"\")) {")
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrInvalid", "proto: "+name+": invalid duration ", conv+".Quote(in)"))
		fg.P("}")
		fg.P("secs, err := ", conv, ".ParseInt(whole, 10, 64)")
		fg.P("if err != nil || secs > ", fg.c("MaxDurationSeconds"), " {")
		fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrOutOfRange", name+": duration out of range"))
		fg.P("}")
		fg.P("var nanos int64")
		fg.P("if dot {")
		fg.P("nanos, _ = ", conv, ".ParseInt(frac, 10, 64)")
		fg.P("for range ", fg.c("MaxFracDigits"), " - len(frac) {")
		fg.P("nanos *= 10")
		fg.P("}")
		fg.P("}")
		fg.P("if neg {")
		fg.P("secs, nanos = -secs, -nanos")
		fg.P("}")
		fg.P(S, ", ", N, " = secs, int32(nanos)")
		fg.P("return nil")
	})
}

func (fg *fileGen) genJSONWrapper(mi *messageInfo) {
	fi, idx := fieldByName(mi, "value")
	fg.genJSONMarshal(mi, func() {
		// The getter handles a nil m.
		if e, ok := fg.jsonOutExpr(fi.f, "m."+fi.getter+"()", "b", ""); ok {
			fg.P("return ", e, ", nil")
			return
		}
		fg.jsonOut(fi.f, "m."+fi.getter+"()", "", false)
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.P("if d.PeekKind() == ", fg.kind("Null"), " {")
		fg.P("// JSON null leaves the message unchanged.")
		fg.P("return d.SkipValue()")
		fg.P("}")
		fg.jsonValue(mi, []int{idx})
		fg.P("return nil")
	})
}

// genJSONContainer handles Struct (a JSON object) and ListValue (a JSON
// array), which serialize as their single map or repeated field.
func (fg *fileGen) genJSONContainer(mi *messageInfo, field string, isMap bool) {
	fi, idx := fieldByName(mi, field)
	name := mi.m.FullName
	fv := "m." + fi.goName
	fg.genJSONMarshal(mi, func() {
		open, close := "[", "]"
		if isMap {
			open, close = "{", "}"
		}
		fg.P("if len(m.", fi.getter, "()) == 0 {")
		fg.P("return ", appendLit("b", open+close), ", nil")
		fg.P("}")
		fg.P("b = ", appendLit("b", open))
		if isMap {
			fg.jsonMapEntries(fi.f, fv)
		} else {
			fg.P("for j := range ", fv, " {")
			fg.jsonOut(fi.f, fv+"[j]", "", true)
			fg.P("}")
		}
		fg.P("b[len(b)-1] = '", close, "'")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		end := "EndArray"
		if isMap {
			fg.jsonOpen(name, "BeginObject", "object")
			end = "EndObject"
		} else {
			fg.jsonOpen(name, "BeginArray", "array")
		}
		fg.P("for d.PeekKind() != ", fg.kind(end), " {")
		if isMap {
			fg.P("kt, err := d.ReadToken()")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("mk := kt.String()")
		}
		fg.jsonValue(mi, []int{idx})
		fg.P("}")
		fg.P("_, err = d.ReadToken()")
		fg.P("return err")
	})
}

func (fg *fileGen) genJSONValue(mi *messageInfo) {
	name := mi.m.FullName
	member := func(n string) *fieldInfo { fi, _ := fieldByName(mi, n); return fi }
	null, num, str, bl := member("null_value"), member("number_value"), member("string_value"), member("bool_value")
	st, lst := member("struct_value"), member("list_value")
	kind := null.oneof.goName
	fg.genJSONMarshal(mi, func() {
		fg.P("if m == nil {")
		fg.P("return append(b, \"null\"...), nil")
		fg.P("}")
		fg.P("switch o := m.", kind, ".(type) {")
		fg.P("case *", null.wrapper, ":")
		fg.P("b = append(b, \"null\"...)")
		fg.P("case *", num.wrapper, ":")
		m := fg.std("math")
		fg.P("if ", m, ".IsNaN(o.", num.goName, ") || ", m, ".IsInf(o.", num.goName, ", 0) {")
		fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrNonFinite", name+": number_value cannot be NaN or Infinity"))
		fg.P("}")
		fg.P("b = ", fg.std("encoding/json/jsontext"), ".AppendFloat(b, o.", num.goName, ", 64)")
		fg.P("case *", str.wrapper, ":")
		fg.jsonAppendString("b", "o."+str.goName, str.f.FullName)
		fg.P("case *", bl.wrapper, ":")
		fg.jsonOut(bl.f, "o."+bl.goName, "", false)
		fg.P("case *", st.wrapper, ":")
		fg.jsonOut(st.f, "o."+st.goName, "", false)
		fg.P("case *", lst.wrapper, ":")
		fg.jsonOut(lst.f, "o."+lst.goName, "", false)
		fg.P("default:")
		fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrNoKind", name+": no kind is set"))
		fg.P("}")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		readTok := func() {
			fg.P("tok, err := d.ReadToken()")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
		}
		fg.P("switch d.PeekKind() {")
		fg.P("case ", fg.kind("Null"), ":")
		fg.P("if err := d.SkipValue(); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("m.", kind, " = &", null.wrapper, "{}")
		fg.P("case ", fg.kind("True"), ", ", fg.kind("False"), ":")
		readTok()
		fg.P("m.", kind, " = &", bl.wrapper, "{", bl.goName, ": tok.Bool()}")
		fg.P("case ", fg.kind("String"), ":")
		readTok()
		fg.P("m.", kind, " = &", str.wrapper, "{", str.goName, ": tok.String()}")
		fg.P("case ", fg.kind("BeginObject"), ":")
		fg.P("x := &", fg.msgType(st.f.MessageType), "{}")
		fg.P("if err := x.ProtoMergeJSONFrom(d); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("m.", kind, " = &", st.wrapper, "{", st.goName, ": x}")
		fg.P("case ", fg.kind("BeginArray"), ":")
		fg.P("x := &", fg.msgType(lst.f.MessageType), "{}")
		fg.P("if err := x.ProtoMergeJSONFrom(d); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("m.", kind, " = &", lst.wrapper, "{", lst.goName, ": x}")
		fg.P("case ", fg.kind("Number"), ":")
		readTok()
		fg.P("fv, err := ", fg.std("strconv"), ".ParseFloat(tok.String(), 64)")
		fg.P("if err != nil {")
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrInvalidNumber", "proto: "+name+": invalid number ", "tok.String()"))
		fg.P("}")
		fg.P("m.", kind, " = &", num.wrapper, "{", num.goName, ": fv}")
		fg.P("default:")
		fg.P("// A syntax error, which the read reports.")
		fg.P("if _, err := d.ReadToken(); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrInvalidJSON", name+": invalid JSON"))
		fg.P("}")
		fg.P("return nil")
	})
}

func (fg *fileGen) genJSONFieldMask(mi *messageInfo) {
	name := mi.m.FullName
	paths, _ := fieldByName(mi, "paths")
	fv := "m." + paths.goName
	fg.genJSONMarshal(mi, func() {
		fg.P("b = append(b, '\"')")
		fg.P("if m != nil {")
		fg.P("for j, p := range ", fv, " {")
		fg.P("if j > 0 {")
		fg.P("b = append(b, ',')")
		fg.P("}")
		fg.P("// snake_case to lowerCamelCase; reject paths that do not round-trip.")
		fg.P("for ci := 0; ci < len(p); ci++ {")
		fg.P("switch c := p[ci]; {")
		fg.P("case c == '_':")
		fg.P("if ci+1 >= len(p) || p[ci+1] < 'a' || p[ci+1] > 'z' {")
		fg.P("return nil, ", fg.errConcat(fg.owner(name)+"ErrUnrepresentablePath", "proto: "+name+": path cannot be represented in JSON: ", fg.std("strconv")+".Quote(p)"))
		fg.P("}")
		fg.P("ci++")
		fg.P("b = append(b, p[ci]-('a'-'A'))")
		fg.P("case c >= 'A' && c <= 'Z', c < ' ', c == '\"', c == '\\\\', c == ',':")
		fg.P("return nil, ", fg.errConcat(fg.owner(name)+"ErrUnrepresentablePath", "proto: "+name+": path cannot be represented in JSON: ", fg.std("strconv")+".Quote(p)"))
		fg.P("default:")
		fg.P("b = append(b, c)")
		fg.P("}")
		fg.P("}")
		fg.P("}")
		fg.P("}")
		fg.P("b = append(b, '\"')")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.jsonReadString(name)
		fg.P("if s == \"\" {")
		fg.P("return nil")
		fg.P("}")
		fg.P("for _, p := range ", fg.std("strings"), ".Split(s, \",\") {")
		fg.P("// lowerCamelCase to snake_case.")
		fg.P("var sb []byte")
		fg.P("for ci := 0; ci < len(p); ci++ {")
		fg.P("switch c := p[ci]; {")
		fg.P("case c == '_':")
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrInvalidPath", "proto: "+name+": invalid path ", fg.std("strconv")+".Quote(p)"))
		fg.P("case c >= 'A' && c <= 'Z':")
		fg.P("sb = append(sb, '_', c+('a'-'A'))")
		fg.P("default:")
		fg.P("sb = append(sb, c)")
		fg.P("}")
		fg.P("}")
		fg.P(fv, " = append(", fv, ", string(sb))")
		fg.P("}")
		fg.P("return nil")
	})
}

func (fg *fileGen) genJSONAny(mi *messageInfo) {
	const msg = "google.protobuf.Any: JSON requires a type registry, which cotorp-generated code does not have"
	fg.genJSONMarshal(mi, func() {
		fg.P("return nil, ", fg.jsonErr(fg.owner(mi.m.FullName)+"ErrUnsupported", msg))
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.P("if d.PeekKind() == ", fg.kind("Null"), " {")
		fg.P("return d.SkipValue()")
		fg.P("}")
		fg.P("return ", fg.jsonErr(fg.owner(mi.m.FullName)+"ErrUnsupported", msg))
	})
}
