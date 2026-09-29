// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"fmt"
	"slices"

	"github.com/canonical/cotorp/internal/desc"
)

// This file generates the wire-format methods. Each field is sized and
// decoded by one call to a helper function (see helpers.go), and written by
// one call where an inlinable writer exists for its kind and tag size
// (implicit-presence scalars); other fields are written inline under their
// presence check.

// --- Size -----------------------------------------------------------------

func (fg *fileGen) genSize(mi *messageInfo) {
	fg.P("// ProtoSize returns the size of the wire-format encoding of m.")
	fg.P("func (m *", mi.goName, ") ProtoSize() (n int) {")
	fg.P("m = m.z()")
	done := map[*oneofInfo]bool{}
	for _, fi := range mi.byNum {
		if oi := fi.oneof; oi != nil {
			if done[oi] {
				continue
			}
			done[oi] = true
			fg.P("switch o := m.", oi.goName, ".(type) {")
			for _, of := range oi.fields {
				fg.P("case *", of.wrapper, ":")
				fg.P("n += ", fg.sizeSingular(of.f, "o."+of.goName))
			}
			fg.P("}")
			continue
		}
		fg.sizeField(fi)
	}
	fg.P("return n + len(m.unknownFields)")
	fg.P("}")
	fg.P()
}

// sizeSingular returns an expression for the size of a present singular
// value v of field f, with its tag.
func (fg *fileGen) sizeSingular(f *desc.Field, v string) string {
	ts := tagSize(f.Number, wireType(f))
	switch {
	case f.Kind == desc.KindMessage && f.Delimited:
		return fmt.Sprint(2*ts, " + ", v, ".ProtoSize()")
	case f.Kind == desc.KindMessage:
		return fmt.Sprint(ts, " + ", fg.fn("SizeLen"), "(", v, ".ProtoSize())")
	}
	return fmt.Sprint(ts, " + ", fg.sizeExpr(f.Kind, v))
}

// sizeFunc returns a function literal calling ProtoSize on a message of
// f's type. After the size helper is inlined, the compiler calls the method
// directly; a method expression would be called through a func value.
func (fg *fileGen) sizeFunc(f *desc.Field) string {
	return "func(v " + fg.scalarGoType(f) + ") int { return v.ProtoSize() }"
}

// lenFunc returns the helper computing the encoded size of a value of kind
// k without its tag, for the kinds whose size is not fixed.
func (fg *fileGen) lenFunc(k desc.Kind) string {
	switch k {
	case desc.KindSint32:
		return fg.fn("Sint32Len")
	case desc.KindSint64:
		return fg.fn("Sint64Len")
	case desc.KindString, desc.KindBytes:
		return fg.fn("BytesLen")
	case desc.KindBool:
		return fg.fn("BoolLen")
	}
	return fg.fn("VarintLen")
}

func (fg *fileGen) sizeField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	ts := tagSize(f.Number, wireType(f))
	switch {
	case f.IsMap:
		key, val := f.MapKey, f.MapValue
		k, v := "k", "v"
		if fixedSize(key.Kind) > 0 {
			k = "_"
		}
		var vsize string
		if val.Kind == desc.KindMessage {
			vsize = fg.fn("SizeLen") + "(v.ProtoSize())"
		} else {
			vsize = fg.sizeExpr(val.Kind, "v")
			if fixedSize(val.Kind) > 0 {
				v = "_"
			}
		}
		switch {
		case k == "_" && v == "_":
			fg.P("for range ", fv, " {")
		case v == "_":
			fg.P("for ", k, " := range ", fv, " {")
		default:
			fg.P("for ", k, ", ", v, " := range ", fv, " {")
		}
		fg.P("n += ", ts, " + ", fg.fn("SizeLen"), "(", tagSize(1, wireType(key)), " + ", fg.sizeExpr(key.Kind, "k"), " + ", tagSize(2, wireType(val)), " + ", vsize, ")")
		fg.P("}")
	case f.Repeated && f.Packed:
		pts := tagSize(f.Number, wireBytes)
		switch fs := fixedSize(f.Kind); {
		case fs == 1:
			fg.P("n += ", fg.fn("SizeLenField"), "(", pts, ", len(", fv, "))")
		case fs > 0:
			fg.P("n += ", fg.fn("SizeLenField"), "(", pts, ", len(", fv, ")*", fg.fixedSizeExpr(f.Kind), ")")
		default:
			fg.P("n += ", fg.fn("SizePacked"), "(", pts, ", ", fv, ", ", fg.lenFunc(f.Kind), ")")
		}
	case f.Repeated && f.Kind == desc.KindMessage && f.Delimited:
		fg.P("for _, v := range ", fv, " {")
		fg.P("n += ", fg.sizeSingular(f, "v"))
		fg.P("}")
	case f.Repeated && f.Kind == desc.KindMessage:
		fg.P("n += ", fg.fn("SizeMsgs"), "(", ts, ", ", fv, ", ", fg.sizeFunc(f), ")")
	case f.Repeated && fixedSize(f.Kind) > 0:
		fg.P("n += len(", fv, ") * (", ts, " + ", fg.fixedSizeExpr(f.Kind), ")")
	case f.Repeated:
		fg.P("n += ", fg.fn("SizeEach"), "(", ts, ", ", fv, ", ", fg.lenFunc(f.Kind), ")")
	case f.Kind == desc.KindMessage && f.Delimited:
		fg.P("if ", fv, " != nil {")
		fg.P("n += ", fg.sizeSingular(f, fv))
		fg.P("}")
	case f.Kind == desc.KindMessage:
		fg.P("n += ", fg.fn("SizeMsg"), "(", ts, ", ", fv, ", ", fg.sizeFunc(f), ")")
	case f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("n += ", fg.fn("SizePresentBytes"), "(", ts, ", ", fv, ")")
	case f.HasPresence && fixedSize(f.Kind) > 0:
		fg.P("n += ", fg.fn("SizeOptFixed"), "(", ts, ", ", fg.fixedSizeExpr(f.Kind), ", ", fv, ")")
	case f.HasPresence:
		fg.P("n += ", fg.fn("SizeOpt"), "(", ts, ", ", fv, ", ", fg.lenFunc(f.Kind), ")")
	case f.Kind == desc.KindBool:
		fg.P("n += ", fg.fn("SizeBool"), "(", ts, ", ", fv, ")")
	case fixedSize(f.Kind) > 0:
		fg.P("n += ", fg.fn("SizeFixed"), "(", ts, ", ", fg.fixedSizeExpr(f.Kind), ", ", fg.nonZeroExpr(f.Kind, fv), ")")
	case f.Kind == desc.KindString, f.Kind == desc.KindBytes:
		fg.P("n += ", fg.fn("SizeLenField"), "(", ts, ", len(", fv, "))")
	default:
		fg.P("n += ", fg.fn("SizeVarint"), "(", ts, ", ", varintExpr(f.Kind, fv), ")")
	}
}

// --- Marshal --------------------------------------------------------------

func (fg *fileGen) genMarshal(mi *messageInfo) {
	name := mi.goName

	fg.P("// MarshalBinary returns the wire-format encoding of m.")
	fg.P("func (m *", name, ") MarshalBinary() ([]byte, error) { return m.AppendBinary(nil) }")
	fg.P()
	fg.P("// AppendBinary appends the wire-format encoding of m to b.")
	fg.P("func (m *", name, ") AppendBinary(b []byte) ([]byte, error) {")
	if mi.m.HasRequired() {
		fg.P("if err := m.ProtoCheckInitialized(); err != nil {")
		fg.P("return b, err")
		fg.P("}")
	}
	fg.P("return ", fg.fn("AppendBinary"), "(b, m.ProtoSize(), m.ProtoMarshalToSizedBuffer)")
	fg.P("}")
	fg.P()

	fg.P("// ProtoMarshalToSizedBuffer encodes m into the end of b, which must hold m.ProtoSize() bytes, and returns the count written, without checking required fields.")
	fg.P("func (m *", name, ") ProtoMarshalToSizedBuffer(b []byte) (int, error) {")
	fg.P("m = m.z()")
	fg.P("i := ", fg.fn("CopyUnknown"), "(b, len(b), m.unknownFields)")
	fg.checkStrings(mi, "0, err", func(f *desc.Field) bool { return f.ValidateUTF8 })
	for _, fi := range slices.Backward(mi.byNum) {
		if fi.oneof != nil {
			fg.P("if o, ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
			fg.marshalSingular(fi.f, "o."+fi.goName, true)
			fg.P("}")
			continue
		}
		fg.marshalField(fi)
	}
	fg.P("return len(b) - i, nil")
	fg.P("}")
	fg.P()
}

// checkStrings emits one check that the singular string fields of mi
// selected by validate hold valid UTF-8, returning ret on failure. Repeated
// and map strings are checked where they are written. Fields with presence
// are read through their getters, which handle nil pointers.
func (fg *fileGen) checkStrings(mi *messageInfo, ret string, validate func(*desc.Field) bool) {
	chain := "nil"
	for _, fi := range mi.fields {
		f := fi.f
		if f.Kind != desc.KindString || f.IsMap || f.Repeated || !validate(f) {
			continue
		}
		v := "m." + fi.goName
		if fi.oneof != nil || f.HasPresence {
			v = "m." + fi.getter + "()"
		}
		chain = fg.fn("CheckUTF8") + "(" + v + ", " + fg.utf8Err(f) + ", " + chain + ")"
	}
	if chain == "nil" {
		return
	}
	fg.P("if err := ", chain, "; err != nil {")
	fg.P("return ", ret)
	fg.P("}")
}

// utf8Err returns the string expression for f's invalid UTF-8 error message.
func (fg *fileGen) utf8Err(f *desc.Field) string {
	return fg.errString(fg.owner(f.FullName)+"ErrUTF8", fmt.Sprintf(errInvalidUTF8F, f.FullName))
}

// tag returns a constant expression for the tag of field f with wire type
// wt.
func (fg *fileGen) tag(f *desc.Field, wt int) string {
	return fg.tagExpr(fg.fieldNum(f), wt)
}

// putLen returns an expression writing a length prefix for the bytes
// written since index start, then the tag of field f, and evaluating to the
// new i.
func (fg *fileGen) putLen(f *desc.Field) string {
	return fg.putVarint(fg.putVarint("i", "uint64(start-i)"), fg.tag(f, wireBytes))
}

// marshalSingular emits code writing present value v (with tag) of field f.
// checked reports that strings were validated by checkStrings.
func (fg *fileGen) marshalSingular(f *desc.Field, v string, checked bool) {
	if f.Kind == desc.KindMessage {
		if f.Delimited {
			fg.P("i = ", fg.putVarint("i", fg.tag(f, wireEndGroup)))
		}
		fg.P("n, err := ", v, ".ProtoMarshalToSizedBuffer(b[:i])")
		fg.P("if err != nil {")
		fg.P("return 0, err")
		fg.P("}")
		if f.Delimited {
			fg.P("i = ", fg.putVarint("i-n", fg.tag(f, wireStartGroup)))
		} else {
			fg.P("i = ", fg.putVarint(fg.putVarint("i-n", "uint64(n)"), fg.tag(f, wireBytes)))
		}
		return
	}
	if f.Kind == desc.KindString && f.ValidateUTF8 && !checked {
		fg.P("if !", fg.std("unicode/utf8"), ".ValidString(", v, ") {")
		fg.P("return 0, ", fg.std("errors"), ".New(", fg.utf8Err(f), ")")
		fg.P("}")
	}
	fg.P("i = ", fg.putVarint(fg.putScalar(f.Kind, v), fg.tag(f, wireType(f))))
}

func (fg *fileGen) marshalField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	switch {
	case f.IsMap:
		entry := func() {
			fg.P("start := i")
			fg.marshalSingular(f.MapValue, "v", false)
			fg.marshalSingular(f.MapKey, "k", false)
			fg.P("i = ", fg.putLen(f))
		}
		fg.P("if len(", fv, ") > 0 {")
		if f.MapKey.Kind == desc.KindBool {
			// Written back to front, so true precedes false.
			fg.P("for _, k := range [2]bool{true, false} {")
			fg.P("v, ok := ", fv, "[k]")
			fg.P("if !ok {")
			fg.P("continue")
			fg.P("}")
			entry()
			fg.P("}")
		} else {
			fg.P("for _, k := range ", fg.std("slices"), ".Backward(", fg.fn("SortedKeys"), "(", fv, ", make([]", fg.scalarGoType(f.MapKey), ", 0, len(", fv, ")))) {")
			fg.P("v := ", fv, "[k]")
			entry()
			fg.P("}")
		}
		fg.P("}")
	case f.Repeated && f.Packed:
		fg.P("if len(", fv, ") > 0 {")
		fg.P("start := i")
		fg.P("for _, v := range ", fg.std("slices"), ".Backward(", fv, ") {")
		fg.P("i = ", fg.putScalar(f.Kind, "v"))
		fg.P("}")
		fg.P("i = ", fg.putLen(f))
		fg.P("}")
	case f.Repeated:
		fg.P("for _, v := range ", fg.std("slices"), ".Backward(", fv, ") {")
		fg.marshalSingular(f, "v", false)
		fg.P("}")
	case f.Kind == desc.KindMessage, f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.marshalSingular(f, fv, true)
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.marshalSingular(f, "(*"+fv+")", true)
		fg.P("}")
	default:
		if call, ok := fg.putField(f, fv); ok {
			fg.P("i = ", call)
			return
		}
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.marshalSingular(f, fv, true)
		fg.P("}")
	}
}

// putField returns a call writing implicit-presence scalar field f (value
// fv) with its tag if the value is set, when an inlinable helper exists for
// its kind and tag size (see genPutVarintFieldN).
func (fg *fileGen) putField(f *desc.Field, fv string) (string, bool) {
	tag := fg.tag(f, wireType(f))
	var h, v string
	switch f.Kind {
	case desc.KindString:
		h, v = "PutStringField", fv
	case desc.KindBytes:
		h, v = "PutBytesField", fv
	case desc.KindBool:
		h, v = "PutBoolField", fv
	case desc.KindDouble:
		h, v = "PutFixed64Field", fg.std("math")+".Float64bits("+fv+")"
	case desc.KindFixed64, desc.KindSfixed64:
		h, v = "PutFixed64Field", "uint64("+fv+")"
	case desc.KindFloat:
		h, v = "PutFixed32Field", fg.std("math")+".Float32bits("+fv+")"
	case desc.KindFixed32, desc.KindSfixed32:
		h, v = "PutFixed32Field", "uint32("+fv+")"
	case desc.KindMessage:
		return "", false
	default:
		h, v = "PutVarintField", varintExpr(f.Kind, fv)
	}
	switch tagSize(f.Number, wireType(f)) {
	case 1:
	case 2:
		if f.Kind == desc.KindString || f.Kind == desc.KindBytes {
			return "", false
		}
		h += "2"
	default:
		return "", false
	}
	return fg.fn(h) + "(b, i, " + v + ", " + tag + ")", true
}

// --- Unmarshal ------------------------------------------------------------

// ProtoMergeDepth reads one tag per iteration and decodes the field with a
// helper that returns the length consumed, or zero or less if the value is
// malformed. Decoding that can fail with a specific error (invalid UTF-8,
// nested messages) also sets err. Both are checked once, after the switch.
func (fg *fileGen) genUnmarshal(mi *messageInfo) {
	name := mi.goName
	bin := fg.std("encoding/binary")

	merge := "m.ProtoMergeDepth(b, 0)"
	if mi.m.HasRequired() {
		merge = fg.std("cmp") + ".Or(m.ProtoMergeDepth(b, 0), m.ProtoCheckInitialized())"
	}
	fg.P("// UnmarshalBinary replaces the contents of m with the wire-format message in b.")
	fg.P("func (m *", name, ") UnmarshalBinary(b []byte) error { *m = ", name, "{}; return ", merge, " }")
	fg.P()
	fg.P("// ProtoMerge decodes the wire-format message in b and merges it into m, without checking required fields.")
	fg.P("func (m *", name, ") ProtoMerge(b []byte) error { return m.ProtoMergeDepth(b, 0) }")
	fg.P()
	fg.usesX = false
	cases := fg.capture(func() {
		for _, fi := range mi.byNum {
			fg.unmarshalField(fi)
		}
	})
	fg.P("// ProtoMergeDepth is ProtoMerge for a message nested depth levels deep.")
	fg.P("func (m *", name, ") ProtoMergeDepth(b []byte, depth int) error {")
	// The depth error stops the loop before its first iteration.
	if fg.usesX {
		fg.P("var x, err = []byte(nil), ", fg.fn("Depth"), "(depth)")
	} else {
		fg.P("err := ", fg.fn("Depth"), "(depth)")
	}
	fg.P("for t, n := ", bin, ".Uvarint(b); n > 0 && err == nil; t, n = ", bin, ".Uvarint(b) {")
	fg.P("b = b[n:]")
	if len(mi.byNum) > 0 {
		fg.P("switch t {")
		fg.buf.WriteString(cases)
		fg.P("default:")
	}
	fg.P("n, err = ", fg.fn("Unknown"), "(b, t, depth, &m.unknownFields)")
	if len(mi.byNum) > 0 {
		fg.P("}")
	}
	fg.emitDecodeCheck("n")
	fg.P("b = b[n:]")
	fg.P("}")
	fg.P("return ", fg.fn("End"), "(b, err)")
	fg.P("}")
	fg.P()
}

// emitDecodeCheck emits the check of a decoder's result: length n and err.
func (fg *fileGen) emitDecodeCheck(n string) {
	fg.P("if ", n, " <= 0 || err != nil {")
	fg.P("return ", fg.std("cmp"), ".Or(err, ", fg.errExpr("ErrParse", errParseMsg), ")")
	fg.P("}")
}

func isClosedEnum(f *desc.Field) bool {
	return f.Kind == desc.KindEnum && f.EnumType.Closed
}

// decoder returns the helper call decoding a value of f's kind from buf
// into the pointer expression dst, and whether it also returns an error.
func (fg *fileGen) decoder(f *desc.Field, buf, dst string) (string, bool) {
	var h string
	switch f.Kind {
	case desc.KindString:
		msg := `""`
		if f.ValidateUTF8 {
			msg = fg.utf8Err(f)
		}
		return fg.fn("String") + "(" + buf + ", " + dst + ", " + msg + ")", true
	case desc.KindBytes:
		h = "Bytes"
	case desc.KindBool:
		h = "Bool"
	case desc.KindSint32:
		h = "Sint32"
	case desc.KindSint64:
		h = "Sint64"
	case desc.KindFixed32, desc.KindSfixed32:
		h = "Fixed32"
	case desc.KindFixed64, desc.KindSfixed64:
		h = "Fixed64"
	case desc.KindFloat:
		h = "Float"
	case desc.KindDouble:
		h = "Double"
	default:
		h = "Varint"
	}
	return fg.fn(h) + "(" + buf + ", " + dst + ")", false
}

// packedDecoder returns the helper call decoding a packed field of f's kind
// from b into the slice at dst.
func (fg *fileGen) packedDecoder(f *desc.Field, dst string) string {
	var h string
	switch f.Kind {
	case desc.KindBool:
		h = "PackedBool"
	case desc.KindSint32:
		h = "PackedSint32"
	case desc.KindSint64:
		h = "PackedSint64"
	case desc.KindFixed32, desc.KindSfixed32:
		h = "PackedFixed32"
	case desc.KindFixed64, desc.KindSfixed64:
		h = "PackedFixed64"
	case desc.KindFloat:
		h = "PackedFloat"
	case desc.KindDouble:
		h = "PackedDouble"
	default:
		h = "PackedVarint"
	}
	return fg.fn(h) + "(b, " + dst + ")"
}

// unmarshalField emits the cases of the ProtoMergeDepth tag switch that
// decode field fi.
func (fg *fileGen) unmarshalField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	num := fg.fieldNum(f)

	switch {
	case f.IsMap:
		fg.unmarshalMap(fi)
		return
	case f.Kind == desc.KindMessage:
		fg.usesX = true
		if f.Delimited {
			fg.P("case ", fg.tagExpr(num, wireStartGroup), ":")
			fg.P("x, n, err = ", fg.fn("Group"), "(b, ", num, ", depth)")
		} else {
			fg.P("case ", fg.tagExpr(num, wireBytes), ":")
			fg.P("x, n = ", fg.fn("ReadBytes"), "(b)")
		}
		// Obtain the target message, then merge into it. x is nil after a
		// failure, which merges nothing.
		var mv string
		switch {
		case fi.oneof != nil:
			mv = "mv"
			fg.P("var mv *", fg.msgType(f.MessageType))
			fg.P("if o, ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok && o.", fi.goName, " != nil {")
			fg.P("mv = o.", fi.goName)
			fg.P("} else {")
			fg.P("mv = &", fg.msgType(f.MessageType), "{}")
			fg.P("m.", fi.oneof.goName, " = &", fi.wrapper, "{", fi.goName, ": mv}")
			fg.P("}")
		case f.Repeated:
			mv = fg.fn("Alloc") + "(" + fg.fn("Grow") + "(&" + fv + "))"
		default:
			mv = fg.fn("Alloc") + "(&" + fv + ")"
		}
		if f.Delimited {
			fg.P("err = ", fg.std("cmp"), ".Or(err, ", mv, ".ProtoMergeDepth(x, depth+1))")
		} else {
			fg.P("err = ", mv, ".ProtoMergeDepth(x, depth+1)")
		}
		return
	}

	if isClosedEnum(f) {
		fg.unmarshalClosedEnum(fi)
		return
	}
	if f.Repeated && f.Kind.Packable() {
		// Accept packed encoding regardless of the declared form.
		fg.P("case ", fg.tagExpr(num, wireBytes), ":")
		fg.P("n = ", fg.packedDecoder(f, "&"+fv))
	}
	fg.P("case ", fg.tagExpr(num, wireType(f)), ":")
	switch {
	case fi.oneof != nil:
		fg.P("o := new(", fi.wrapper, ")")
		if call, hasErr := fg.decoder(f, "b", "&o."+fi.goName); hasErr {
			fg.P("n, err = ", call)
			fg.P("m.", fi.oneof.goName, " = o")
		} else {
			fg.P("n, m.", fi.oneof.goName, " = ", call, ", o")
		}
		return
	case f.Repeated:
		fv = fg.fn("Grow") + "(&" + fv + ")"
	case f.HasPresence && f.Kind != desc.KindBytes:
		fv = fg.fn("New") + "(&" + fv + ")"
	default:
		fv = "&" + fv
	}
	if call, hasErr := fg.decoder(f, "b", fv); hasErr {
		fg.P("n, err = ", call)
	} else {
		fg.P("n = ", call)
	}
}

// unmarshalClosedEnum emits the cases decoding closed enum field fi, whose
// unrecognized values are kept as unknown fields by the ClosedEnum helpers.
func (fg *fileGen) unmarshalClosedEnum(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	num := fg.fieldNum(f)
	var set string
	switch {
	case fi.oneof != nil:
		set = "m." + fi.oneof.goName + " = &" + fi.wrapper + "{" + fi.goName + ": e}"
	case f.Repeated:
		set = fv + " = append(" + fv + ", e)"
	case f.HasPresence:
		set = fv + " = new(e)"
	default:
		set = fv + " = e"
	}
	args := "(b, &m.unknownFields, " + fg.tagExpr(num, wireVarint) + ", func(e " + fg.enumType(f.EnumType) + ") { " + set + " })"
	if f.Repeated {
		fg.P("case ", fg.tagExpr(num, wireBytes), ":")
		fg.P("n = ", fg.fn("PackedClosedEnum"), args)
	}
	fg.P("case ", fg.tagExpr(num, wireVarint), ":")
	fg.P("n = ", fg.fn("ClosedEnum"), args)
}

// typedZero returns the zero value of scalar field f's Go type as a typed
// expression.
func (fg *fileGen) typedZero(f *desc.Field) string {
	switch f.Kind {
	case desc.KindBool:
		return "false"
	case desc.KindString:
		return `""`
	case desc.KindBytes:
		return "[]byte(nil)"
	case desc.KindMessage:
		return "(" + fg.scalarGoType(f) + ")(nil)"
	}
	return fg.scalarGoType(f) + "(0)"
}

// unmarshalMap emits the ProtoMergeDepth case that decodes one entry of map
// field fi, with the same loop as a message.
func (fg *fileGen) unmarshalMap(fi *fieldInfo) {
	f := fi.f
	key, val := f.MapKey, f.MapValue
	bin := fg.std("encoding/binary")
	fg.usesX = true
	fg.P("case ", fg.tagExpr(fg.fieldNum(f), wireBytes), ":")
	fg.P("x, n = ", fg.fn("ReadBytes"), "(b)")
	fg.P("mk, mv := ", fg.typedZero(key), ", ", fg.typedZero(val))
	if val.Kind == desc.KindMessage {
		fg.P("var v []byte")
	}
	fg.P("for t, k := ", bin, ".Uvarint(x); k > 0; t, k = ", bin, ".Uvarint(x) {")
	fg.P("x = x[k:]")
	fg.P("switch t {")
	fg.P("case ", fg.tagExpr(fg.fieldNum(key), wireType(key)), ":")
	if call, hasErr := fg.decoder(key, "x", "&mk"); hasErr {
		fg.P("k, err = ", call)
	} else {
		fg.P("k = ", call)
	}
	fg.P("case ", fg.tagExpr(fg.fieldNum(val), wireType(val)), ":")
	if val.Kind == desc.KindMessage {
		fg.P("v, k = ", fg.fn("ReadBytes"), "(x)")
		fg.P("err = ", fg.fn("Alloc"), "(&mv).ProtoMergeDepth(v, depth+1)")
	} else if call, hasErr := fg.decoder(val, "x", "&mv"); hasErr {
		fg.P("k, err = ", call)
	} else {
		fg.P("k = ", call)
	}
	fg.P("default:")
	fg.P("k, err = ", fg.fn("SkipField"), "(x, t, depth)")
	fg.P("}")
	fg.emitDecodeCheck("k")
	fg.P("x = x[k:]")
	fg.P("}")
	if val.Kind == desc.KindMessage {
		fg.P(fg.fn("Alloc"), "(&mv)")
	}
	set := fg.fn("MapSet") + "(&m." + fi.goName + ", mk, mv)"
	if isClosedEnum(val) {
		fg.P("if mv.IsValid() {")
		fg.P(set)
		fg.P("} else {")
		fg.P("m.unknownFields = append(", bin, ".AppendUvarint(m.unknownFields, t), b[:n]...)")
		fg.P("}")
	} else {
		fg.P(set)
	}
	fg.P("err = ", fg.fn("End"), "(x, err)")
}

// --- Required fields --------------------------------------------------------

func (fg *fileGen) genCheckInitialized(mi *messageInfo) {
	fg.P("// ProtoCheckInitialized returns an error if a required field of m or of a message in m is not set.")
	if !mi.m.HasRequired() {
		fg.P("func (m *", mi.goName, ") ProtoCheckInitialized() error { return nil }")
		fg.P()
		return
	}
	fg.P("func (m *", mi.goName, ") ProtoCheckInitialized() error {")
	// Unset sub-messages are checked through nil receivers, which pass.
	fg.P("if m == nil {")
	fg.P("return nil")
	fg.P("}")
	last := "" // the call in the last check emitted, if it can be returned
	for _, fi := range mi.byNum {
		f := fi.f
		fv := "m." + fi.goName
		if f.Required {
			last = ""
			fg.P("if ", fv, " == nil {")
			fg.P("return ", fg.errExpr(fg.owner(f.FullName)+"ErrRequired", fmt.Sprintf(errRequiredFmt, f.FullName)))
			fg.P("}")
		}
		var sub *desc.Message
		if f.IsMap {
			sub = f.MapValue.MessageType
		} else {
			sub = f.MessageType
		}
		if sub == nil || !sub.HasRequired() {
			continue
		}
		switch {
		case f.IsMap, f.Repeated:
			fg.P("for _, v := range ", fv, " {")
			fg.P("if err := v.ProtoCheckInitialized(); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("}")
		case fi.oneof != nil:
			fg.P("if o, ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
			fg.P("if err := o.", fi.goName, ".ProtoCheckInitialized(); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("}")
		default:
			last = fv + ".ProtoCheckInitialized()"
			fg.P("if err := ", last, "; err != nil {")
			fg.P("return err")
			fg.P("}")
			continue
		}
		last = ""
	}
	if last != "" {
		// The final check's result is the method's result.
		fg.buf.Truncate(fg.buf.Len() - len("if err := "+last+"; err != nil {\nreturn err\n}\n"))
		fg.P("return ", last)
	} else {
		fg.P("return nil")
	}
	fg.P("}")
	fg.P()
}
