// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/canonical/cotorp/internal/desc"
)

// This file generates ProtoJSON (https://protobuf.dev/programming-guides/json/)
// methods. As with the binary methods, all logic is inlined and only the
// standard library and other generated messages' methods are called.
//
// Unmarshaling runs in two phases. Phase one tokenizes the JSON object,
// maps keys to field indexes, rejects duplicates, unknown keys and oneof
// conflicts, and flattens arrays and map entries into "jobs". Phase two
// parses each job's raw value using one shared block per scalar class (so
// the number and base64 parsing code appears once per message rather than
// once per field) and then assigns it to the field.

// Scalar parse classes used by phase two.
const (
	classNone     = 0
	classSigned   = 1
	classUnsigned = 2
	classFloat    = 3
	classBool     = 4
	classString   = 5
	classBytes    = 6
	classHex      = 7 // bytes fields selected with Options.JSONHex
)

// jsonQuote returns the JSON encoding of s as a Go string literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
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

// classConst names the constant for a phase-two parse class.
var classConst = [...]string{
	classNone:     "ClassNone",
	classSigned:   "ClassSigned",
	classUnsigned: "ClassUnsigned",
	classFloat:    "ClassFloat",
	classBool:     "ClassBool",
	classString:   "ClassString",
	classBytes:    "ClassBytes",
	classHex:      "ClassHex",
}

// class returns the constant naming parse class c.
func (fg *fileGen) class(c int) string {
	return fg.c(classConst[c])
}

// --- Marshal ----------------------------------------------------------------

// jsonAppendString emits code appending v as a JSON string. name is used in
// the invalid UTF-8 error.
func (fg *fileGen) jsonAppendString(v, name string) {
	fg.P("if !", fg.std("unicode/utf8"), ".ValidString(", v, ") {")
	fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrJSONUTF8", name+" contains invalid UTF-8"))
	fg.P("}")
	fg.P("b = append(b, '\"')")
	fg.P("for ci := 0; ci < len(", v, "); ci++ {")
	fg.P("switch c := ", v, "[ci]; {")
	fg.P("case c == '\"' || c == '\\\\':")
	fg.P("b = append(b, '\\\\', c)")
	fg.P("case c < ' ':")
	hd := fg.c("HexDigits")
	fg.P("b = append(b, '\\\\', 'u', '0', '0', ", hd, "[c>>4], ", hd, "[c&0xf])")
	fg.P("default:")
	fg.P("b = append(b, c)")
	fg.P("}")
	fg.P("}")
	fg.P("b = append(b, '\"')")
}

// jsonAppendValue emits code appending the JSON form of singular value v of
// field f to b.
func (fg *fileGen) jsonAppendValue(f *desc.Field, v string) {
	conv := fg.std("strconv")
	switch f.Kind {
	case desc.KindInt32, desc.KindSint32, desc.KindSfixed32:
		fg.P("b = ", conv, ".AppendInt(b, int64(", v, "), 10)")
	case desc.KindUint32, desc.KindFixed32:
		fg.P("b = ", conv, ".AppendUint(b, uint64(", v, "), 10)")
	case desc.KindInt64, desc.KindSint64, desc.KindSfixed64:
		fg.P("b = append(b, '\"')")
		fg.P("b = ", conv, ".AppendInt(b, ", v, ", 10)")
		fg.P("b = append(b, '\"')")
	case desc.KindUint64, desc.KindFixed64:
		fg.P("b = append(b, '\"')")
		fg.P("b = ", conv, ".AppendUint(b, ", v, ", 10)")
		fg.P("b = append(b, '\"')")
	case desc.KindBool:
		fg.P("if ", v, " {")
		fg.P("b = append(b, \"true\"...)")
		fg.P("} else {")
		fg.P("b = append(b, \"false\"...)")
		fg.P("}")
	case desc.KindFloat, desc.KindDouble:
		bits := "64"
		if f.Kind == desc.KindFloat {
			bits = "32"
		}
		m := fg.std("math")
		fg.P("switch fl := float64(", v, "); {")
		fg.P("case ", m, ".IsNaN(fl):")
		fg.P("b = append(b, `\"NaN\"`...)")
		fg.P("case ", m, ".IsInf(fl, 1):")
		fg.P("b = append(b, `\"Infinity\"`...)")
		fg.P("case ", m, ".IsInf(fl, -1):")
		fg.P("b = append(b, `\"-Infinity\"`...)")
		fg.P("default:")
		fg.P("b = ", conv, ".AppendFloat(b, fl, 'g', -1, ", bits, ")")
		fg.P("}")
	case desc.KindString:
		fg.jsonAppendString(v, f.FullName)
	case desc.KindBytes:
		if fg.g.jsonHex[f.FullName] {
			hx := fg.std("encoding/hex")
			fg.P("b = append(b, '\"')")
			fg.P("l := len(b)")
			fg.P("b = ", fg.std("slices"), ".Grow(b, ", hx, ".EncodedLen(len(", v, ")))[:l+", hx, ".EncodedLen(len(", v, "))]")
			fg.P(hx, ".Encode(b[l:], ", v, ")")
			fg.P("b = append(b, '\"')")
			return
		}
		b64 := fg.std("encoding/base64")
		fg.P("n := ", b64, ".StdEncoding.EncodedLen(len(", v, "))")
		fg.P("b = append(b, '\"')")
		fg.P("l := len(b)")
		fg.P("b = ", fg.std("slices"), ".Grow(b, n)[:l+n]")
		fg.P(b64, ".StdEncoding.Encode(b[l:], ", v, ")")
		fg.P("b = append(b, '\"')")
	case desc.KindEnum:
		if f.EnumType.FullName == "google.protobuf.NullValue" {
			fg.P("b = append(b, \"null\"...)")
			return
		}
		if fg.g.opts.JSONEnumNumbers {
			fg.P("b = ", conv, ".AppendInt(b, int64(", v, "), 10)")
			return
		}
		fg.P("if s, ok := ", fg.qualify(f.EnumType.File, fg.g.enmNames[f.EnumType]+"_name"), "[int32(", v, ")]; ok {")
		fg.P("b = append(b, '\"')")
		fg.P("b = append(b, s...)")
		fg.P("b = append(b, '\"')")
		fg.P("} else {")
		fg.P("b = ", conv, ".AppendInt(b, int64(", v, "), 10)")
		fg.P("}")
	case desc.KindMessage:
		fg.P("var err error")
		fg.P("if b, err = ", v, ".ProtoAppendJSON(b); err != nil {")
		fg.P("return nil, err")
		fg.P("}")
	}
}

// jsonAppendMapKey emits code appending map key k as a quoted JSON string.
func (fg *fileGen) jsonAppendMapKey(key *desc.Field, name string) {
	conv := fg.std("strconv")
	switch key.Kind {
	case desc.KindString:
		fg.jsonAppendString("k", name)
	case desc.KindBool:
		fg.P("if k {")
		fg.P("b = append(b, `\"true\"`...)")
		fg.P("} else {")
		fg.P("b = append(b, `\"false\"`...)")
		fg.P("}")
	case desc.KindUint32, desc.KindFixed32, desc.KindUint64, desc.KindFixed64:
		fg.P("b = append(b, '\"')")
		fg.P("b = ", conv, ".AppendUint(b, uint64(k), 10)")
		fg.P("b = append(b, '\"')")
	default:
		fg.P("b = append(b, '\"')")
		fg.P("b = ", conv, ".AppendInt(b, int64(k), 10)")
		fg.P("b = append(b, '\"')")
	}
}

// jsonAppendField emits code appending `"name":value,` for a populated
// non-oneof field.
func (fg *fileGen) jsonAppendField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	key := strconv.Quote(jsonQuote(f.JSONName) + ":")
	switch {
	case f.IsMap:
		fg.P("if len(", fv, ") > 0 {")
		fg.P("b = append(b, ", strconv.Quote(jsonQuote(f.JSONName)+":{"), "...)")
		fg.jsonMapEntries(f, fv)
		fg.P("b[len(b)-1] = '}'")
		fg.P("b = append(b, ',')")
		fg.P("}")
	case f.Repeated:
		fg.P("if len(", fv, ") > 0 {")
		fg.P("b = append(b, ", strconv.Quote(jsonQuote(f.JSONName)+":["), "...)")
		fg.P("for j := range ", fv, " {")
		fg.jsonAppendValue(f, fv+"[j]")
		fg.P("b = append(b, ',')")
		fg.P("}")
		fg.P("b[len(b)-1] = ']'")
		fg.P("b = append(b, ',')")
		fg.P("}")
	case f.Kind == desc.KindMessage, f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.P("b = append(b, ", key, "...)")
		fg.jsonAppendValue(f, fv)
		fg.P("b = append(b, ',')")
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.P("b = append(b, ", key, "...)")
		fg.jsonAppendValue(f, "(*"+fv+")")
		fg.P("b = append(b, ',')")
		fg.P("}")
	default:
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.P("b = append(b, ", key, "...)")
		fg.jsonAppendValue(f, fv)
		fg.P("b = append(b, ',')")
		fg.P("}")
	}
}

// jsonMapEntries emits `"key":value,` for each entry of map fv, in key order.
func (fg *fileGen) jsonMapEntries(f *desc.Field, fv string) {
	entry := func() {
		fg.jsonAppendMapKey(f.MapKey, f.FullName)
		fg.P("b = append(b, ':')")
		fg.jsonAppendValue(f.MapValue, "v")
		fg.P("b = append(b, ',')")
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
	fg.P("keys := make([]", fg.scalarGoType(f.MapKey), ", 0, len(", fv, "))")
	fg.P("for k := range ", fv, " {")
	fg.P("keys = append(keys, k)")
	fg.P("}")
	fg.P(fg.std("slices"), ".Sort(keys)")
	fg.P("for _, k := range keys {")
	fg.P("v := ", fv, "[k]")
	entry()
	fg.P("}")
}

func (fg *fileGen) genJSONMarshal(mi *messageInfo, body func()) {
	name := mi.goName
	fg.P("// MarshalJSON returns the ProtoJSON encoding of m.")
	fg.P("func (m *", name, ") MarshalJSON() ([]byte, error) {")
	if mi.m.HasRequired() {
		fg.P("if err := m.ProtoCheckInitialized(); err != nil {")
		fg.P("return nil, err")
		fg.P("}")
	}
	fg.P("return m.ProtoAppendJSON(nil)")
	fg.P("}")
	fg.P()
	fg.P("// ProtoAppendJSON appends the ProtoJSON encoding of m to b. It does not")
	fg.P("// check required fields.")
	fg.P("func (m *", name, ") ProtoAppendJSON(b []byte) ([]byte, error) {")
	body()
	fg.P("}")
	fg.P()
}

// genericJSONMarshalBody emits the body of ProtoAppendJSON for an ordinary
// message.
func (fg *fileGen) genericJSONMarshalBody(mi *messageInfo) {
	fg.P("if m == nil {")
	fg.P("return append(b, \"{}\"...), nil")
	fg.P("}")
	fg.P("b = append(b, '{')")
	for _, fi := range mi.fields {
		if fi.oneof != nil {
			v := "o"
			if fi.f.Kind == desc.KindEnum && fi.f.EnumType.FullName == "google.protobuf.NullValue" {
				v = "_" // always written as null
			}
			fg.P("if ", v, ", ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
			fg.P("b = append(b, ", strconv.Quote(jsonQuote(fi.f.JSONName)+":"), "...)")
			fg.jsonAppendValue(fi.f, "o."+fi.goName)
			fg.P("b = append(b, ',')")
			fg.P("}")
			continue
		}
		fg.jsonAppendField(fi)
	}
	fg.P("if b[len(b)-1] == ',' {")
	fg.P("b[len(b)-1] = '}'")
	fg.P("} else {")
	fg.P("b = append(b, '}')")
	fg.P("}")
	fg.P("return b, nil")
}

// --- Unmarshal --------------------------------------------------------------

func (fg *fileGen) genJSONUnmarshal(mi *messageInfo, body func()) {
	name := mi.goName
	fg.P("// UnmarshalJSON replaces the contents of m with the decoded ProtoJSON")
	fg.P("// value in b.")
	fg.P("func (m *", name, ") UnmarshalJSON(b []byte) error {")
	fg.P("*m = ", name, "{}")
	if mi.m.HasRequired() {
		fg.P("if err := m.ProtoMergeJSON(b); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("return m.ProtoCheckInitialized()")
	} else {
		fg.P("return m.ProtoMergeJSON(b)")
	}
	fg.P("}")
	fg.P()
	fg.P("// ProtoMergeJSON decodes the ProtoJSON value in b and merges it into m.")
	fg.P("// It does not check required fields.")
	fg.P("func (m *", name, ") ProtoMergeJSON(b []byte) error {")
	body()
	fg.P("}")
	fg.P()
}

// jsonFieldIndex returns the declaration index of fi in mi.
func jsonFieldIndex(mi *messageInfo, fi *fieldInfo) int {
	for i, x := range mi.fields {
		if x == fi {
			return i
		}
	}
	panic("field not in message")
}

// jsonJobType declares the job type and slice used between the phases.
func (fg *fileGen) jsonJobType() {
	fg.P("type job struct {")
	fg.P("f   int")
	fg.P("key string")
	fg.P("raw []byte")
	fg.P("}")
}

// genericJSONUnmarshalBody emits the body of ProtoMergeJSON for an ordinary
// message.
func (fg *fileGen) genericJSONUnmarshalBody(mi *messageInfo) {
	name := mi.m.FullName
	jsonPkg := fg.std("encoding/json")
	fg.P("d := ", jsonPkg, ".NewDecoder(", fg.std("bytes"), ".NewReader(b))")
	fg.P("d.UseNumber()")
	fg.P("tok, err := d.Token()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if tok == nil {")
	fg.P("// JSON null leaves the message unchanged.")
	fg.P("if _, err := d.Token(); err != ", fg.std("io"), ".EOF {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrTrailingData", name+": unexpected data after JSON value"))
	fg.P("}")
	fg.P("return nil")
	fg.P("}")
	fg.P("if tok != ", jsonPkg, ".Delim('{') {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrNotObject", name+": expected a JSON object"))
	fg.P("}")
	if len(mi.fields) == 0 {
		if fg.g.opts.JSONDiscardUnknown {
			fg.P("for d.More() {")
			fg.P("if _, err := d.Token(); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("var raw ", jsonPkg, ".RawMessage")
			fg.P("if err := d.Decode(&raw); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("}")
		} else {
			fg.P("if d.More() {")
			fg.P("tok, _ := d.Token()")
			fg.P("key, _ := tok.(string)")
			fg.P("return ", fg.errConcat(fg.owner(name)+"ErrUnknownField", "proto: "+name+": unknown field ", fg.std("strconv")+".Quote(key)"))
			fg.P("}")
		}
		fg.jsonCheckEnd(name)
		fg.P("return nil")
		return
	}
	fg.jsonJobType()
	fg.P("var jobs []job")
	fg.P("var seen [", len(mi.fields), "]bool")
	if len(mi.oneofs) > 0 {
		fg.P("var oneofs [", len(mi.oneofs), "]bool")
	}
	fg.P("for d.More() {")
	fg.P("tok, err := d.Token()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("key, _ := tok.(string)")
	fg.P("var raw ", jsonPkg, ".RawMessage")
	fg.P("if err := d.Decode(&raw); err != nil {")
	fg.P("return err")
	fg.P("}")

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
	fg.P("f := -1")
	fg.P("switch key {")
	for i := range mi.fields {
		if ks := byIdx[i]; len(ks) > 0 {
			fg.P("case ", joinComma(ks), ":")
			fg.P("f = ", i)
		}
	}
	fg.P("default:")
	if fg.g.opts.JSONDiscardUnknown {
		fg.P("continue // unknown keys are ignored (-json_discard_unknown)")
	} else {
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrUnknownField", "proto: "+name+": unknown field ", fg.std("strconv")+".Quote(key)"))
	}
	fg.P("}")
	fg.P("if seen[f] {")
	fg.P("return ", fg.errConcat(fg.owner(name)+"ErrDuplicateField", "proto: "+name+": duplicate field ", fg.std("strconv")+".Quote(key)"))
	fg.P("}")
	fg.P("seen[f] = true")
	// Group fields by structural handling.
	var singular, singularNull, repeated, repeatedNull, maps, mapsNull []int
	oneofMembers := map[int][]int{}
	oneofIdx := map[*oneofInfo]int{}
	for i, oi := range mi.oneofs {
		oneofIdx[oi] = i
	}
	for i, fi := range mi.fields {
		f := fi.f
		switch {
		case fi.oneof != nil:
			oneofMembers[oneofIdx[fi.oneof]] = append(oneofMembers[oneofIdx[fi.oneof]], i)
		case f.IsMap && acceptsNull(f.MapValue):
			mapsNull = append(mapsNull, i)
		case f.IsMap:
			maps = append(maps, i)
		case f.Repeated && acceptsNull(f):
			repeatedNull = append(repeatedNull, i)
		case f.Repeated:
			repeated = append(repeated, i)
		case acceptsNull(f):
			singularNull = append(singularNull, i)
		default:
			singular = append(singular, i)
		}
	}
	needNull := len(singular)+len(repeated)+len(repeatedNull)+len(maps)+len(mapsNull) > 0
	for _, members := range oneofMembers {
		for _, i := range members {
			if !acceptsNull(mi.fields[i].f) {
				needNull = true
			}
		}
	}
	if needNull {
		fg.P("null := string(raw) == \"null\"")
	}
	fg.P("switch f {")
	if len(singular) > 0 {
		fg.P("case ", joinInts(singular), ":")
		fg.P("if !null {")
		fg.P("jobs = append(jobs, job{f: f, raw: raw})")
		fg.P("}")
	}
	if len(singularNull) > 0 {
		fg.P("case ", joinInts(singularNull), ":")
		fg.P("jobs = append(jobs, job{f: f, raw: raw})")
	}
	for k := range mi.oneofs {
		members := oneofMembers[k]
		var plain, withNull []int
		for _, i := range members {
			if acceptsNull(mi.fields[i].f) {
				withNull = append(withNull, i)
			} else {
				plain = append(plain, i)
			}
		}
		fg.P("case ", joinInts(members), ":")
		if len(plain) > 0 && len(withNull) > 0 {
			fg.P("if null && f != ", joinOr("f", withNull), " {")
			fg.P("continue")
			fg.P("}")
		} else if len(withNull) == 0 {
			fg.P("if null {")
			fg.P("continue")
			fg.P("}")
		}
		fg.P("if oneofs[", k, "] {")
		fg.P("return ", fg.jsonErr(fg.owner(name)+camelCase(mi.oneofs[k].o.Name)+"ErrOneofConflict", name+": multiple fields set for oneof "+mi.oneofs[k].o.Name))
		fg.P("}")
		fg.P("oneofs[", k, "] = true")
		fg.P("jobs = append(jobs, job{f: f, raw: raw})")
	}
	collect := func(idxs []int, isMap, nullOK bool) {
		if len(idxs) == 0 {
			return
		}
		fg.P("case ", joinInts(idxs), ":")
		fg.P("if null {")
		fg.P("continue")
		fg.P("}")
		fg.jsonCollect("raw", isMap, nullOK, name)
	}
	collect(repeated, false, false)
	collect(repeatedNull, false, true)
	collect(maps, true, false)
	collect(mapsNull, true, true)
	fg.P("}")
	fg.P("}")
	fg.jsonCheckEnd(name)
	fg.jsonPhase2(mi, allIndexes(mi))
	fg.P("return nil")
}

// jsonCollect emits code splitting a JSON array (or, for maps, object) held
// in rawVar into jobs for field f.
func (fg *fileGen) jsonCollect(rawVar string, isMap, nullOK bool, name string) {
	jsonPkg := fg.std("encoding/json")
	open, what, hint := "[", "array", "ErrNotArray"
	if isMap {
		open, what, hint = "{", "object", "ErrNotObject"
	}
	fg.P("ad := ", jsonPkg, ".NewDecoder(", fg.std("bytes"), ".NewReader(", rawVar, "))")
	fg.P("ad.UseNumber()")
	fg.P("if t, err := ad.Token(); err != nil || t != ", jsonPkg, ".Delim('", open, "') {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+hint, name+": expected a JSON "+what))
	fg.P("}")
	fg.P("for ad.More() {")
	if isMap {
		fg.P("kt, err := ad.Token()")
		fg.P("if err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("ks, _ := kt.(string)")
	}
	fg.P("var e ", jsonPkg, ".RawMessage")
	fg.P("if err := ad.Decode(&e); err != nil {")
	fg.P("return err")
	fg.P("}")
	if !nullOK {
		fg.P("if string(e) == \"null\" {")
		fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrNullElement", name+": null is not allowed in repeated fields or map values"))
		fg.P("}")
	}
	if isMap {
		fg.P("jobs = append(jobs, job{f: f, key: ks, raw: e})")
	} else {
		fg.P("jobs = append(jobs, job{f: f, raw: e})")
	}
	fg.P("}")
}

func (fg *fileGen) jsonCheckEnd(name string) {
	fg.P("if _, err := d.Token(); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if _, err := d.Token(); err != ", fg.std("io"), ".EOF {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrTrailingData", name+": unexpected data after JSON value"))
	fg.P("}")
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

// joinOr returns "a && v != b && ..." style comparisons for f against the
// listed indexes (used to exempt null-accepting oneof members).
func joinOr(v string, xs []int) string {
	s := ""
	for i, x := range xs {
		if i > 0 {
			s += " && " + v + " != "
		}
		s += strconv.Itoa(x)
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

// scalarClass returns the phase-two parse class and bit size for a
// non-message, non-enum kind.
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

// valueField returns the field whose values a job for fi carries: the map
// value for maps, otherwise the field itself.
func valueField(fi *fieldInfo) *desc.Field {
	if fi.f.IsMap {
		return fi.f.MapValue
	}
	return fi.f
}

// jsonPhase2 emits the loop that parses and assigns each job.
func (fg *fileGen) jsonPhase2(mi *messageInfo, idxs []int) {
	name := mi.m.FullName
	used := map[int]bool{}
	hasEnum := false
	for _, i := range idxs {
		vf := valueField(mi.fields[i])
		switch vf.Kind {
		case desc.KindMessage:
		case desc.KindEnum:
			hasEnum = true
			used[classString] = true
			used[classSigned] = true
		default:
			c, _ := fg.fieldClass(vf)
			used[c] = true
		}
	}
	anyScalar := len(used) > 0
	numeric := used[classSigned] || used[classUnsigned] || used[classFloat]

	fg.P("for _, jb := range jobs {")
	fg.P("raw := jb.raw")
	if anyScalar {
		fg.P("class := ", fg.class(classNone))
		if numeric {
			fg.P("bits := 64")
		}
		scratch := map[int]string{classSigned: "iv int64", classUnsigned: "uv uint64", classFloat: "fv float64", classBool: "bv bool", classString: "sv string"}
		for _, c := range []int{classSigned, classUnsigned, classFloat, classBool, classString} {
			if used[c] {
				fg.P("var ", scratch[c])
			}
		}
		if used[classBytes] || used[classHex] {
			fg.P("var by []byte")
		}
		// Select the parse class for the job's field.
		groups := map[[2]int][]int{}
		var enums []int
		for _, i := range idxs {
			vf := valueField(mi.fields[i])
			switch vf.Kind {
			case desc.KindMessage:
			case desc.KindEnum:
				enums = append(enums, i)
			default:
				c, bits := fg.fieldClass(vf)
				groups[[2]int{c, bits}] = append(groups[[2]int{c, bits}], i)
			}
		}
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
		fg.P("switch jb.f {")
		for _, k := range gkeys {
			fg.P("case ", joinInts(groups[k]), ":")
			if numeric && k[1] != 0 {
				fg.P("class, bits = ", fg.class(k[0]), ", ", k[1])
			} else {
				fg.P("class = ", fg.class(k[0]))
			}
		}
		if hasEnum {
			fg.P("case ", joinInts(enums), ":")
			fg.P("switch {")
			fg.P("case string(raw) == \"null\":")
			fg.P("class = ", fg.class(classNone))
			fg.P("case raw[0] == '\"':")
			fg.P("class = ", fg.class(classString))
			fg.P("default:")
			fg.P("class, bits = ", fg.class(classSigned), ", 32")
			fg.P("}")
		}
		fg.P("}")
		fg.jsonParseClasses(used, name)
	}
	fg.P("switch jb.f {")
	for _, i := range idxs {
		fg.P("case ", i, ":")
		fg.jsonAssign(mi.fields[i])
	}
	fg.P("}")
	fg.P("}")
}

// jsonParseClasses emits the shared scalar parse blocks.
func (fg *fileGen) jsonParseClasses(used map[int]bool, name string) {
	jsonPkg := fg.std("encoding/json")
	invalid := func(hint, what string) {
		fg.P("return ", fg.errConcat(fg.owner(name)+hint, "proto: "+name+": invalid "+what+" ", "string(raw)"))
	}
	numberCheck := func() {
		fg.P("if s == \"\" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) || !", jsonPkg, ".Valid([]byte(s)) {")
		invalid("ErrInvalidNumber", "number")
		fg.P("}")
	}
	unquote := func(dst string) {
		fg.P("if raw[0] == '\"' {")
		fg.P("if err := ", jsonPkg, ".Unmarshal(raw, &", dst, "); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("}")
	}
	fg.P("switch class {")
	if used[classSigned] || used[classUnsigned] {
		conv := fg.std("strconv")
		var cases []string
		if used[classSigned] {
			cases = append(cases, fg.class(classSigned))
		}
		if used[classUnsigned] {
			cases = append(cases, fg.class(classUnsigned))
		}
		fg.P("case ", joinComma(cases), ":")
		fg.P("s := string(raw)")
		unquote("s")
		numberCheck()
		fg.P("var err error")
		switch {
		case used[classSigned] && used[classUnsigned]:
			fg.P("if class == ", fg.class(classSigned), " {")
			fg.P("iv, err = ", conv, ".ParseInt(s, 10, bits)")
			fg.P("} else {")
			fg.P("uv, err = ", conv, ".ParseUint(s, 10, bits)")
			fg.P("}")
		case used[classSigned]:
			fg.P("iv, err = ", conv, ".ParseInt(s, 10, bits)")
		default:
			fg.P("uv, err = ", conv, ".ParseUint(s, 10, bits)")
		}
		fg.P("if err != nil {")
		fg.P("// Accept exponent and fraction forms that denote an exact integer,")
		fg.P("// bounding the exponent so that exact arithmetic stays cheap.")
		fg.P("if i := ", fg.std("strings"), ".IndexAny(s, \"eE\"); i >= 0 {")
		fg.P("if e, err := ", conv, ".Atoi(s[i+1:]); err != nil || e > ", fg.c("MaxJSONExponent"), " || e < -", fg.c("MaxJSONExponent"), " {")
		invalid("ErrInvalidInteger", "integer")
		fg.P("}")
		fg.P("}")
		fg.P("r, ok := new(", fg.std("math/big"), ".Rat).SetString(s)")
		fg.P("if !ok || !r.IsInt() {")
		invalid("ErrInvalidInteger", "integer")
		fg.P("}")
		fg.P("n := r.Num()")
		signed := func() {
			m := fg.std("math")
			fg.P("if !n.IsInt64() || (bits == 32 && (n.Int64() < ", m, ".MinInt32 || n.Int64() > ", m, ".MaxInt32)) {")
			invalid("ErrInvalidInteger", "integer")
			fg.P("}")
			fg.P("iv = n.Int64()")
		}
		unsigned := func() {
			fg.P("if !n.IsUint64() || (bits == 32 && n.Uint64() > ", fg.std("math"), ".MaxUint32) {")
			invalid("ErrInvalidInteger", "integer")
			fg.P("}")
			fg.P("uv = n.Uint64()")
		}
		switch {
		case used[classSigned] && used[classUnsigned]:
			fg.P("if class == ", fg.class(classSigned), " {")
			signed()
			fg.P("} else {")
			unsigned()
			fg.P("}")
		case used[classSigned]:
			signed()
		default:
			unsigned()
		}
		fg.P("}")
	}
	if used[classFloat] {
		m := fg.std("math")
		fg.P("case ", fg.class(classFloat), ":")
		fg.P("s := string(raw)")
		fg.P("special := false")
		fg.P("if raw[0] == '\"' {")
		fg.P("if err := ", jsonPkg, ".Unmarshal(raw, &s); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("switch s {")
		fg.P("case \"NaN\":")
		fg.P("fv, special = ", m, ".NaN(), true")
		fg.P("case \"Infinity\":")
		fg.P("fv, special = ", m, ".Inf(1), true")
		fg.P("case \"-Infinity\":")
		fg.P("fv, special = ", m, ".Inf(-1), true")
		fg.P("}")
		fg.P("}")
		fg.P("if !special {")
		numberCheck()
		fg.P("var err error")
		fg.P("if fv, err = ", fg.std("strconv"), ".ParseFloat(s, bits); err != nil {")
		invalid("ErrInvalidNumber", "number")
		fg.P("}")
		fg.P("}")
	}
	if used[classBool] {
		fg.P("case ", fg.class(classBool), ":")
		fg.P("switch string(raw) {")
		fg.P("case \"true\":")
		fg.P("bv = true")
		fg.P("case \"false\":")
		fg.P("default:")
		invalid("ErrInvalidBool", "boolean")
		fg.P("}")
	}
	if used[classString] {
		fg.P("case ", fg.class(classString), ":")
		fg.P("if raw[0] != '\"' || !", fg.std("unicode/utf8"), ".Valid(raw) {")
		invalid("ErrInvalidString", "string")
		fg.P("}")
		fg.P("if err := ", jsonPkg, ".Unmarshal(raw, &sv); err != nil {")
		fg.P("return err")
		fg.P("}")
	}
	if used[classBytes] {
		b64 := fg.std("encoding/base64")
		fg.P("case ", fg.class(classBytes), ":")
		fg.P("var s string")
		fg.P("if raw[0] != '\"' {")
		invalid("ErrInvalidBytes", "bytes")
		fg.P("}")
		fg.P("if err := ", jsonPkg, ".Unmarshal(raw, &s); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("// Accept standard and URL-safe alphabets, with or without padding.")
		fg.P("enc := ", b64, ".StdEncoding")
		fg.P("if ", fg.std("strings"), ".ContainsAny(s, \"-_\") {")
		fg.P("enc = ", b64, ".URLEncoding")
		fg.P("}")
		fg.P("if len(s)%", fg.c("Base64Quantum"), " != 0 {")
		fg.P("enc = enc.WithPadding(", b64, ".NoPadding)")
		fg.P("}")
		fg.P("var err error")
		fg.P("if by, err = enc.DecodeString(s); err != nil {")
		invalid("ErrInvalidBytes", "bytes")
		fg.P("}")
	}
	if used[classHex] {
		fg.P("case ", fg.class(classHex), ":")
		fg.P("var s string")
		fg.P("if raw[0] != '\"' {")
		invalid("ErrInvalidHex", "hex bytes")
		fg.P("}")
		fg.P("if err := ", jsonPkg, ".Unmarshal(raw, &s); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("// Either case is accepted.")
		fg.P("var err error")
		fg.P("if by, err = ", fg.std("encoding/hex"), ".DecodeString(s); err != nil {")
		invalid("ErrInvalidHex", "hex bytes")
		fg.P("}")
	}
	fg.P("}")
}

// scratchExpr converts phase-two scratch variables to f's Go type.
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

// jsonValueExpr emits any statements needed to produce the job's value for
// value field vf and returns an expression for it.
func (fg *fileGen) jsonValueExpr(vf *desc.Field, name string) string {
	switch vf.Kind {
	case desc.KindEnum:
		et := fg.enumType(vf.EnumType)
		fg.P("var ev ", et)
		fg.P("switch class {")
		fg.P("case ", fg.class(classString), ":")
		fg.P("n, ok := ", fg.qualify(vf.EnumType.File, fg.g.enmNames[vf.EnumType]+"_value"), "[sv]")
		fg.P("if !ok {")
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrInvalid"+camelCase(vf.EnumType.Name), "proto: "+name+": invalid value for enum "+vf.EnumType.FullName+": ", fg.std("strconv")+".Quote(sv)"))
		fg.P("}")
		fg.P("ev = ", et, "(n)")
		fg.P("case ", fg.class(classSigned), ":")
		fg.P("ev = ", et, "(iv)")
		fg.P("}")
		return "ev"
	case desc.KindMessage:
		fg.P("mv := &", fg.msgType(vf.MessageType), "{}")
		fg.P("if err := mv.ProtoMergeJSON(raw); err != nil {")
		fg.P("return err")
		fg.P("}")
		return "mv"
	}
	return fg.scratchExpr(vf)
}

// jsonAssign emits the phase-two assignment for one field.
func (fg *fileGen) jsonAssign(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	name := f.Parent.FullName
	switch {
	case f.IsMap:
		key := f.MapKey
		conv := fg.std("strconv")
		badKey := func() {
			fg.P("return ", fg.errConcat(fg.owner(f.FullName)+"ErrInvalidKey", "proto: "+name+": invalid map key for field "+f.Name+": ", conv+".Quote(jb.key)"))
		}
		switch key.Kind {
		case desc.KindString:
			fg.P("k := jb.key")
		case desc.KindBool:
			fg.P("var k bool")
			fg.P("switch jb.key {")
			fg.P("case \"true\":")
			fg.P("k = true")
			fg.P("case \"false\":")
			fg.P("default:")
			badKey()
			fg.P("}")
		default:
			c, bits := scalarClass(key.Kind)
			parse := "ParseInt"
			if c == classUnsigned {
				parse = "ParseUint"
			}
			fg.P("k64, err := ", conv, ".", parse, "(jb.key, 10, ", bits, ")")
			fg.P("if err != nil {")
			badKey()
			fg.P("}")
			fg.P("k := ", fg.scalarGoType(key), "(k64)")
		}
		val := fg.jsonValueExpr(f.MapValue, name)
		fg.P("if ", fv, " == nil {")
		fg.P(fv, " = make(", fg.fieldType(fi), ")")
		fg.P("}")
		fg.P(fv, "[k] = ", val)
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
		fg.P("if err := ", fv, ".ProtoMergeJSON(raw); err != nil {")
		fg.P("return err")
		fg.P("}")
	case f.HasPresence && f.Kind != desc.KindBytes:
		val := fg.jsonValueExpr(f, name)
		fg.P("x := ", val)
		fg.P(fv, " = &x")
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

// jsonTrimmed emits code that sets raw to the trimmed, validated JSON value
// in b, returning early (leaving m unchanged) for null.
func (fg *fileGen) jsonTrimmed(name string) {
	fg.P("raw := ", fg.std("bytes"), ".TrimSpace(b)")
	fg.P("if !", fg.std("encoding/json"), ".Valid(raw) {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrInvalidJSON", name+": invalid JSON"))
	fg.P("}")
	fg.P("if string(raw) == \"null\" {")
	fg.P("return nil")
	fg.P("}")
}

// jsonUnquote emits code decoding raw as a JSON string into s.
func (fg *fileGen) jsonUnquote(name string) {
	fg.P("var s string")
	fg.P("if raw[0] != '\"' || !", fg.std("unicode/utf8"), ".Valid(raw) {")
	fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrNotString", name+": expected a JSON string"))
	fg.P("}")
	fg.P("if err := ", fg.std("encoding/json"), ".Unmarshal(raw, &s); err != nil {")
	fg.P("return err")
	fg.P("}")
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
		fg.jsonTrimmed(name)
		fg.jsonUnquote(name)
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
		fg.jsonTrimmed(name)
		fg.jsonUnquote(name)
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
		fg.P("if m == nil {")
		fg.P("m = &", mi.goName, "{}")
		fg.P("}")
		fg.jsonAppendValue(fi.f, "m."+fi.goName)
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.jsonTrimmed(mi.m.FullName)
		fg.jsonJobType()
		fg.P("jobs := []job{{f: ", idx, ", raw: raw}}")
		fg.jsonPhase2(mi, []int{idx})
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
		open, close := "'['", "']'"
		if isMap {
			open, close = "'{'", "'}'"
		}
		fg.P("b = append(b, ", open, ")")
		fg.P("if m != nil {")
		if isMap {
			fg.jsonMapEntries(fi.f, fv)
		} else {
			fg.P("for j := range ", fv, " {")
			fg.jsonAppendValue(fi.f, fv+"[j]")
			fg.P("b = append(b, ',')")
			fg.P("}")
		}
		fg.P("}")
		fg.P("if b[len(b)-1] == ',' {")
		fg.P("b[len(b)-1] = ", close)
		fg.P("} else {")
		fg.P("b = append(b, ", close, ")")
		fg.P("}")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.jsonTrimmed(name)
		fg.jsonJobType()
		fg.P("var jobs []job")
		fg.P("f := ", idx)
		fg.jsonCollect("raw", isMap, true, name)
		fg.jsonPhase2(mi, []int{idx})
		fg.P("return nil")
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
		fg.P("b = ", fg.std("strconv"), ".AppendFloat(b, o.", num.goName, ", 'g', -1, 64)")
		fg.P("case *", str.wrapper, ":")
		fg.jsonAppendString("o."+str.goName, str.f.FullName)
		fg.P("case *", bl.wrapper, ":")
		fg.jsonAppendValue(bl.f, "o."+bl.goName)
		fg.P("case *", st.wrapper, ":")
		fg.jsonAppendValue(st.f, "o."+st.goName)
		fg.P("case *", lst.wrapper, ":")
		fg.jsonAppendValue(lst.f, "o."+lst.goName)
		fg.P("default:")
		fg.P("return nil, ", fg.jsonErr(fg.owner(name)+"ErrNoKind", name+": no kind is set"))
		fg.P("}")
		fg.P("return b, nil")
	})
	fg.genJSONUnmarshal(mi, func() {
		fg.P("raw := ", fg.std("bytes"), ".TrimSpace(b)")
		fg.P("if !", fg.std("encoding/json"), ".Valid(raw) {")
		fg.P("return ", fg.jsonErr(fg.owner(name)+"ErrInvalidJSON", name+": invalid JSON"))
		fg.P("}")
		fg.P("switch raw[0] {")
		fg.P("case 'n':")
		fg.P("m.", kind, " = &", null.wrapper, "{}")
		fg.P("case 't', 'f':")
		fg.P("m.", kind, " = &", bl.wrapper, "{", bl.goName, ": raw[0] == 't'}")
		fg.P("case '\"':")
		fg.jsonUnquote(name)
		fg.P("m.", kind, " = &", str.wrapper, "{", str.goName, ": s}")
		fg.P("case '{':")
		fg.P("x := &", fg.msgType(st.f.MessageType), "{}")
		fg.P("if err := x.ProtoMergeJSON(raw); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("m.", kind, " = &", st.wrapper, "{", st.goName, ": x}")
		fg.P("case '[':")
		fg.P("x := &", fg.msgType(lst.f.MessageType), "{}")
		fg.P("if err := x.ProtoMergeJSON(raw); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("m.", kind, " = &", lst.wrapper, "{", lst.goName, ": x}")
		fg.P("default:")
		fg.P("fv, err := ", fg.std("strconv"), ".ParseFloat(string(raw), 64)")
		fg.P("if err != nil {")
		fg.P("return ", fg.errConcat(fg.owner(name)+"ErrInvalidNumber", "proto: "+name+": invalid number ", "string(raw)"))
		fg.P("}")
		fg.P("m.", kind, " = &", num.wrapper, "{", num.goName, ": fv}")
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
		fg.jsonTrimmed(name)
		fg.jsonUnquote(name)
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
		fg.P("if string(", fg.std("bytes"), ".TrimSpace(b)) == \"null\" {")
		fg.P("return nil")
		fg.P("}")
		fg.P("return ", fg.jsonErr(fg.owner(mi.m.FullName)+"ErrUnsupported", msg))
	})
}
