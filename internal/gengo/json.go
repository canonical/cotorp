// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"encoding/json/jsontext"
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
// decoder rejects malformed JSON and invalid UTF-8. A message's loop reads
// one key per iteration and calls a field helper (Field, OptField, RepField,
// MapField or OneofField) with the parse function for the field's type; the
// helper rejects duplicate keys, handles null and stores the value. A token
// is only valid until the next read from the decoder, so each value is
// converted before the decoder is used again.

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

// kind returns the jsontext constant for a token kind, such as "Null" or
// "BeginObject".
func (fg *fileGen) kind(name string) string {
	return fg.jt() + ".Kind" + name
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
// false for strings and messages, which need statements (see jsonOut).
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
		// Record only the import that is used.
		var enc string
		if fg.g.jsonHex[f.FullName] {
			enc = fg.std("encoding/hex")
		} else {
			enc = fg.std("encoding/base64") + ".StdEncoding"
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
// v of field f to b. Strings and messages can fail, and pass err on.
func (fg *fileGen) jsonOut(f *desc.Field, v, lit string) {
	if e, ok := fg.jsonOutExpr(f, v, "b", lit); ok {
		fg.P("b = ", e)
		return
	}
	fg.usesErr = true
	if f.Kind == desc.KindString {
		fg.P("b, err = ", fg.fn("Quote"), "(", appendLit("b", lit), ", ", v, ", err)")
		return
	}
	// A function literal, so that the inlined helper calls the method
	// directly (see sizeFunc).
	fg.P("b, err = ", fg.fn("AppendMsg"), "(b, ", strconv.Quote(lit), ", ", v, ", func(v ", fg.scalarGoType(f), ", b []byte) ([]byte, error) { return v.ProtoAppendJSON(b) }, err)")
}

// jsonElemFunc returns a function appending the JSON form of a value of
// field f (a repeated element or map value), for AppendList and AppendMap.
func (fg *fileGen) jsonElemFunc(f *desc.Field) string {
	t := fg.scalarGoType(f)
	switch f.Kind {
	case desc.KindMessage:
		return "(" + t + ").ProtoAppendJSON"
	case desc.KindString:
		return "func(v string, b []byte) ([]byte, error) { return " + fg.jt() + ".AppendQuote(b, v) }"
	}
	e, _ := fg.jsonOutExpr(f, "v", "b", "")
	return "func(v " + t + ", b []byte) ([]byte, error) { return " + e + ", nil }"
}

// jsonKeyFunc returns a function literal appending map key k of field f as
// a JSON string.
func (fg *fileGen) jsonKeyFunc(f *desc.Field) string {
	k := f.MapKey
	t := fg.scalarGoType(k)
	conv := fg.std("strconv")
	var e string
	switch k.Kind {
	case desc.KindString:
		return "func(k string, b []byte) ([]byte, error) { return " + fg.jt() + ".AppendQuote(b, k) }"
	case desc.KindUint32, desc.KindFixed32, desc.KindUint64, desc.KindFixed64:
		e = conv + ".AppendUint(append(b, '\"'), uint64(k), 10)"
	default:
		e = conv + ".AppendInt(append(b, '\"'), int64(k), 10)"
	}
	return "func(k " + t + ", b []byte) ([]byte, error) { return append(" + e + ", '\"'), nil }"
}

// jsonAppendMap emits code appending the entries of map field f, in key
// order, after the literal open.
func (fg *fileGen) jsonAppendMap(f *desc.Field, fv, open string) {
	if f.MapKey.Kind == desc.KindBool {
		// Not cmp.Ordered, so not sortable by AppendMap.
		fg.P("if len(", fv, ") > 0 {")
		fg.P("b = ", appendLit("b", open))
		fg.P("for _, k := range [2]bool{false, true} {")
		fg.P("v, ok := ", fv, "[k]")
		fg.P("if !ok {")
		fg.P("continue")
		fg.P("}")
		fg.P("b = ", fg.std("strconv"), ".AppendBool(append(b, '\"'), k)")
		fg.jsonOut(f.MapValue, "v", `":`)
		fg.P("b = append(b, ',')")
		fg.P("}")
		fg.P("b[len(b)-1] = '}'")
		fg.P("}")
		return
	}
	fg.usesErr = true
	fg.P("b, err = ", fg.fn("AppendMap"), "(b, ", strconv.Quote(open), ", ", fv, ", ", fg.jsonKeyFunc(f), ", ", fg.jsonElemFunc(f.MapValue), ", err)")
}

// jsonAppendList emits code appending the elements of repeated field f
// after the literal open.
func (fg *fileGen) jsonAppendList(f *desc.Field, fv, open string) {
	fg.usesErr = true
	fg.P("b, err = ", fg.fn("AppendList"), "(b, ", strconv.Quote(open), ", ", fv, ", ", fg.jsonElemFunc(f), ", err)")
}

// jsonAppendField emits code appending `,"name":value` for a populated
// non-oneof field.
func (fg *fileGen) jsonAppendField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	key := "," + jsonQuote(f.JSONName) + ":"
	switch {
	case f.IsMap:
		fg.jsonAppendMap(f, fv, key+"{")
	case f.Repeated:
		fg.jsonAppendList(f, fv, key+"[")
	case f.Kind == desc.KindMessage:
		fg.jsonOut(f, fv, key)
	case f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.jsonOut(f, fv, key)
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.jsonOut(f, "(*"+fv+")", key)
		fg.P("}")
	default:
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.jsonOut(f, fv, key)
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
	fg.P("// MarshalJSONTo writes the ProtoJSON encoding of m to e, implementing json.MarshalerTo from encoding/json/v2.")
	fg.P("func (m *", name, ") MarshalJSONTo(e *", fg.jt(), ".Encoder) error {")
	checkInit("err")
	fg.P("return ", fg.fn("MarshalTo"), "(e, m.ProtoAppendJSON)")
	fg.P("}")
	fg.P()
	fg.usesErr = false
	code := fg.capture(body)
	fg.P("// ProtoAppendJSON appends the ProtoJSON encoding of m to b, without checking required fields.")
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
	fg.P("m, start := m.z(), len(b)")
	for _, fi := range mi.fields {
		if fi.oneof != nil {
			v := "o"
			if fi.f.Kind == desc.KindEnum && fi.f.EnumType.FullName == "google.protobuf.NullValue" {
				v = "_" // always written as null
			}
			fg.P("if ", v, ", ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
			fg.jsonOut(fi.f, "o."+fi.goName, ","+jsonQuote(fi.f.JSONName)+":")
			fg.P("}")
			continue
		}
		fg.jsonAppendField(fi)
	}
	if fg.usesErr {
		fg.P("return ", fg.fn("CloseObject"), "(b, start), err")
	} else {
		fg.P("return ", fg.fn("CloseObject"), "(b, start), nil")
	}
}

// --- Unmarshal --------------------------------------------------------------

func (fg *fileGen) genJSONUnmarshal(mi *messageInfo, body func()) {
	name := mi.goName
	full := mi.m.FullName
	jt := fg.jt()
	merge := func(call string) string {
		if mi.m.HasRequired() {
			// cmp.Or returns the first error; the check is cheap.
			return fg.std("cmp") + ".Or(" + call + ", m.ProtoCheckInitialized())"
		}
		return call
	}
	fg.P("// UnmarshalJSON replaces the contents of m with the ProtoJSON value in b.")
	fg.P("func (m *", name, ") UnmarshalJSON(b []byte) error { *m = ", name, "{}; return ", merge("m.ProtoMergeJSON(b)"), " }")
	fg.P()
	fg.P("// ProtoMergeJSON decodes the ProtoJSON value in b and merges it into m, without checking required fields.")
	fg.P("func (m *", name, ") ProtoMergeJSON(b []byte) error {")
	fg.P("return ", fg.fn("MergeJSON"), "(b, ", strconv.Quote(full), ", m.ProtoMergeJSONFrom)")
	fg.P("}")
	fg.P()
	fg.P("// UnmarshalJSONFrom replaces the contents of m with the ProtoJSON value read from d, implementing json.UnmarshalerFrom from encoding/json/v2.")
	fg.P("func (m *", name, ") UnmarshalJSONFrom(d *", jt, ".Decoder) error { *m = ", name, "{}; return ", merge(fg.fn("MergeFrom")+"(d, m.ProtoMergeJSONFrom)"), " }")
	fg.P()
	fg.P("// ProtoMergeJSONFrom decodes one ProtoJSON value from d and merges it into m, without checking required fields; d should reject invalid UTF-8, as jsontext decoders do by default.")
	fg.P("func (m *", name, ") ProtoMergeJSONFrom(d *", jt, ".Decoder) error {")
	body()
	fg.P("}")
	fg.P()
	for _, d := range fg.decls {
		fg.P(d)
		fg.P()
	}
	fg.decls = nil
}

// jsonReadOpen emits code reading the first token of a value, which must be
// of the given kind; null leaves m unchanged. It declares tok and err.
func (fg *fileGen) jsonReadOpen(name, kind, hint, what string) {
	fg.P("tok, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if tok.Kind() == ", fg.kind("Null"), " {")
	fg.P("return nil")
	fg.P("}")
	fg.P("if tok.Kind() != ", fg.kind(kind), " {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+hint, name+": expected a JSON "+what))
	fg.P("}")
}

// jsonUnknown emits code rejecting unknown field key, or skipping its value
// with Options.JSONDiscardUnknown, assigning any error to err.
func (fg *fileGen) jsonUnknown(name, key string) {
	if fg.g.opts.JSONDiscardUnknown {
		fg.P("err = d.SkipValue()")
		return
	}
	fg.P("return ", fg.errConcat(fg.owner(name)+"ErrUnknownField", "proto: "+name+": unknown field ", fg.std("strconv")+".Quote("+key+")"))
}

// enumJSONFunc returns the name of the function parsing a JSON value of enum
// e, declaring it after the current message if needed. Each enum used by
// the file gets one, so that enum fields take the same parse function type
// as the other kinds.
func (fg *fileGen) enumJSONFunc(e *desc.Enum) string {
	if name, ok := fg.enumJSON[e]; ok {
		return name
	}
	name := fg.constName("J" + fg.owner(e.FullName))
	fg.enumJSON[e] = name
	values := fg.qualify(e.File, fg.g.enmNames[e]+"_value")
	fg.decls = append(fg.decls, "func "+name+"(d *"+fg.jt()+".Decoder, p *"+fg.enumType(e)+", name string) error {\nreturn "+fg.fn("JSONEnum")+"(d, p, "+values+", name, "+strconv.Quote(e.FullName)+", "+strconv.FormatBool(e.FullName == "google.protobuf.NullValue")+")\n}")
	return name
}

// jsonParser returns the function parsing one JSON value of field f's
// type (the map value's type for maps) into a pointer to its Go type.
func (fg *fileGen) jsonParser(f *desc.Field) string {
	switch f.Kind {
	case desc.KindEnum:
		return fg.enumJSONFunc(f.EnumType)
	case desc.KindMessage:
		if acceptsNull(f) {
			return fg.fn("JSONMsgNull")
		}
		return fg.fn("JSONMsg")
	case desc.KindInt32, desc.KindSint32, desc.KindSfixed32, desc.KindInt64, desc.KindSint64, desc.KindSfixed64:
		return fg.fn("JSONInt")
	case desc.KindUint32, desc.KindFixed32, desc.KindUint64, desc.KindFixed64:
		return fg.fn("JSONUint")
	case desc.KindFloat, desc.KindDouble:
		return fg.fn("JSONFloat")
	case desc.KindBool:
		return fg.fn("JSONBool")
	case desc.KindString:
		return fg.fn("JSONString")
	case desc.KindBytes:
		if fg.g.jsonHex[f.FullName] {
			return fg.fn("JSONHex")
		}
		return fg.fn("JSONBytes")
	}
	panic("jsonParser: " + f.Kind.String())
}

// jsonKeyParser returns the function parsing a map key of field f.
func (fg *fileGen) jsonKeyParser(f *desc.Field) string {
	switch f.MapKey.Kind {
	case desc.KindString:
		return fg.fn("StringKey")
	case desc.KindBool:
		return fg.fn("BoolKey")
	case desc.KindUint32, desc.KindFixed32, desc.KindUint64, desc.KindFixed64:
		return fg.fn("UintKey")
	}
	return fg.fn("IntKey")
}

// genericJSONUnmarshalBody emits the body of ProtoMergeJSONFrom for an
// ordinary message: one loop reading a key and calling a field helper
// (see genField) that checks for duplicates and parses the value.
func (fg *fileGen) genericJSONUnmarshalBody(mi *messageInfo) {
	name := mi.m.FullName
	q := strconv.Quote(name)
	// The loop reads the first key with OpenObject and the rest with
	// NextKey, which passes an error on as an invalid token; the body
	// returns it. It ends on the closing brace, also returned for null.
	loop := func() {
		fg.P("for kt, err := ", fg.fn("OpenObject"), "(d, ", q, "); kt.Kind() != ", fg.kind("EndObject"), "; kt, err = ", fg.fn("NextKey"), "(d, err) {")
		fg.P("if err != nil {")
		fg.P("return err")
		fg.P("}")
	}
	if len(mi.fields) == 0 {
		loop()
		fg.jsonUnknown(name, "kt.String()")
		fg.P("}")
		fg.P("return nil")
		return
	}

	oneofIdx := map[*oneofInfo]int{}
	for i, oi := range mi.oneofs {
		oneofIdx[oi] = i
	}
	if len(mi.oneofs) > 0 {
		fg.P("seen, oneofs := [", len(mi.fields), "]bool{}, [", len(mi.oneofs), "]bool{}")
	} else {
		fg.P("var seen [", len(mi.fields), "]bool")
	}
	loop()

	// Keys: JSON names take precedence over proto names, as in protojson.
	claimed := map[string]bool{}
	keys := map[int][]string{}
	for i, fi := range mi.fields {
		if !claimed[fi.f.JSONName] {
			claimed[fi.f.JSONName] = true
			keys[i] = append(keys[i], strconv.Quote(fi.f.JSONName))
		}
	}
	for i, fi := range mi.fields {
		if !claimed[fi.f.Name] {
			claimed[fi.f.Name] = true
			keys[i] = append(keys[i], strconv.Quote(fi.f.Name))
		}
	}
	fg.P("switch key := kt.String(); key {")
	for i, fi := range mi.fields {
		if len(keys[i]) == 0 {
			continue
		}
		f := fi.f
		fv := "m." + fi.goName
		seen := "&seen[" + strconv.Itoa(i) + "]"
		fg.P("case ", joinComma(keys[i]), ":")
		switch {
		case f.IsMap:
			fg.P("err = ", fg.fn("MapField"), "(d, ", seen, ", key, ", q, ", ", strconv.Quote(f.Name), ", &", fv, ", ", acceptsNull(f.MapValue), ", ", fg.jsonKeyParser(f), ", ", fg.jsonParser(f.MapValue), ")")
		case f.Repeated:
			fg.P("err = ", fg.fn("RepField"), "(d, ", seen, ", key, ", q, ", &", fv, ", ", acceptsNull(f), ", ", fg.jsonParser(f), ")")
		case fi.oneof != nil:
			// The wrapper is converted to the oneof interface, so that
			// type inference does not see two types for I.
			oi := fi.oneof
			fg.P("o := new(", fi.wrapper, ")")
			fg.P("err = ", fg.fn("OneofField"), "(d, ", seen, ", &oneofs[", oneofIdx[oi], "], key, ", q, ", ", strconv.Quote(oi.o.Name), ", &m.", oi.goName, ", ", oi.iface, "(o), &o.", fi.goName, ", ", acceptsNull(f), ", ", fg.jsonParser(f), ")")
		case f.HasPresence && f.Kind != desc.KindMessage && f.Kind != desc.KindBytes && acceptsNull(f):
			// null is a value, so the field is allocated before parsing.
			fg.P("err = ", fg.fn("Field"), "(d, ", seen, ", key, ", q, ", ", fg.fn("New"), "(&", fv, "), ", fg.jsonParser(f), ")")
		case f.HasPresence && f.Kind != desc.KindMessage && f.Kind != desc.KindBytes:
			fg.P("err = ", fg.fn("OptField"), "(d, ", seen, ", key, ", q, ", &", fv, ", ", fg.jsonParser(f), ")")
		default:
			fg.P("err = ", fg.fn("Field"), "(d, ", seen, ", key, ", q, ", &", fv, ", ", fg.jsonParser(f), ")")
		}
	}
	fg.P("default:")
	fg.jsonUnknown(name, "key")
	fg.P("}")
	fg.P("}")
	fg.P("return nil")
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
	fi, _ := fieldByName(mi, "value")
	fg.genJSONMarshal(mi, func() {
		// The getter handles a nil m.
		if e, ok := fg.jsonOutExpr(fi.f, "m."+fi.getter+"()", "b", ""); ok {
			fg.P("return ", e, ", nil")
			return
		}
		fg.P("return ", fg.fn("Quote"), "(b, m.", fi.getter, "(), nil)")
	})
	fg.genJSONUnmarshal(mi, func() {
		// The parser leaves m unchanged on null.
		fg.P("return ", fg.jsonParser(fi.f), "(d, &m.", fi.goName, ", ", strconv.Quote(mi.m.FullName), ")")
	})
}

// genJSONContainer handles Struct (a JSON object) and ListValue (a JSON
// array), which serialize as their single map or repeated field.
func (fg *fileGen) genJSONContainer(mi *messageInfo, field string, isMap bool) {
	fi, _ := fieldByName(mi, field)
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
		if isMap {
			fg.P("return ", fg.fn("AppendMap"), "(b, ", strconv.Quote(open), ", ", fv, ", ", fg.jsonKeyFunc(fi.f), ", ", fg.jsonElemFunc(fi.f.MapValue), ", nil)")
		} else {
			fg.P("return ", fg.fn("AppendList"), "(b, ", strconv.Quote(open), ", ", fv, ", ", fg.jsonElemFunc(fi.f), ", nil)")
		}
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.P("if d.PeekKind() == ", fg.kind("Null"), " {")
		fg.P("return d.SkipValue()")
		fg.P("}")
		if isMap {
			fg.P("return ", fg.fn("JSONMap"), "(d, &", fv, ", ", strconv.Quote(name), ", ", strconv.Quote(field), ", true, ", fg.jsonKeyParser(fi.f), ", ", fg.jsonParser(fi.f.MapValue), ")")
		} else {
			fg.P("return ", fg.fn("JSONArray"), "(d, &", fv, ", ", strconv.Quote(name), ", true, ", fg.jsonParser(fi.f), ")")
		}
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
		fg.P("return ", fg.fn("Quote"), "(b, o.", str.goName, ", nil)")
		fg.P("case *", bl.wrapper, ":")
		fg.jsonOut(bl.f, "o."+bl.goName, "")
		fg.P("case *", st.wrapper, ":")
		fg.jsonOut(st.f, "o."+st.goName, "")
		fg.P("case *", lst.wrapper, ":")
		fg.jsonOut(lst.f, "o."+lst.goName, "")
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
