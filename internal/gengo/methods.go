// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"fmt"
	"slices"

	"github.com/canonical/cotorp/internal/desc"
)

// This file generates the wire-format methods. Encoding and decoding logic
// is emitted inline, except for code shared by every message (writing
// varints, reading length-delimited values and skipping unknown fields),
// which calls the file's helper functions (see helpers.go).

// --- Size -----------------------------------------------------------------

func (fg *fileGen) genSize(mi *messageInfo) {
	fg.P("// ProtoSize returns the size of the wire-format encoding of m.")
	fg.P("func (m *", mi.goName, ") ProtoSize() (n int) {")
	fg.P("if m == nil {")
	fg.P("return 0")
	fg.P("}")
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
				fg.sizeSingular(of.f, "o."+of.goName)
			}
			fg.P("}")
			continue
		}
		fg.sizeField(fi)
	}
	fg.P("n += len(m.unknownFields)")
	fg.P("return n")
	fg.P("}")
	fg.P()
}

// sizeSingular emits code adding the size of a present singular value v.
func (fg *fileGen) sizeSingular(f *desc.Field, v string) {
	ts := tagSize(f.Number, wireType(f))
	switch {
	case f.Kind == desc.KindMessage && f.Delimited:
		fg.P("n += ", 2*ts, " + ", v, ".ProtoSize()")
	case f.Kind == desc.KindMessage:
		fg.P("n += ", ts, " + ", fg.fn("SizeLen"), "(", v, ".ProtoSize())")
	default:
		fg.P("n += ", ts, " + ", fg.sizeExpr(f.Kind, v))
	}
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
		fg.P("if len(", fv, ") > 0 {")
		if fs := fixedSize(f.Kind); fs == 1 {
			fg.P("n += ", pts, " + ", fg.fn("SizeLen"), "(len(", fv, "))")
		} else if fs > 0 {
			fg.P("n += ", pts, " + ", fg.fn("SizeLen"), "(len(", fv, ") * ", fg.fixedSizeExpr(f.Kind), ")")
		} else {
			fg.P("l := 0")
			fg.P("for _, v := range ", fv, " {")
			fg.P("l += ", fg.sizeExpr(f.Kind, "v"))
			fg.P("}")
			fg.P("n += ", pts, " + ", fg.fn("SizeLen"), "(l)")
		}
		fg.P("}")
	case f.Repeated:
		if fixedSize(f.Kind) > 0 {
			fg.P("n += len(", fv, ") * (", ts, " + ", fg.fixedSizeExpr(f.Kind), ")")
			return
		}
		fg.P("for _, v := range ", fv, " {")
		fg.sizeSingular(f, "v")
		fg.P("}")
	case f.Kind == desc.KindMessage, f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.sizeSingular(f, fv)
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.sizeSingular(f, "(*"+fv+")")
		fg.P("}")
	default:
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.sizeSingular(f, fv)
		fg.P("}")
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
	fg.P("size := m.ProtoSize()")
	fg.P("b = ", fg.std("slices"), ".Grow(b, size)")
	fg.P("n, err := m.ProtoMarshalToSizedBuffer(b[len(b) : len(b)+size])")
	fg.P("return ", fg.fn("Appended"), "(b, size, n, err)")
	fg.P("}")
	fg.P()

	body := fg.capture(func() {
		for _, fi := range slices.Backward(mi.byNum) {
			if fi.oneof != nil {
				fg.P("if o, ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
				fg.marshalSingular(fi.f, "o."+fi.goName)
				fg.P("}")
				continue
			}
			fg.marshalField(fi)
		}
	})
	fg.P("// ProtoMarshalToSizedBuffer encodes m into the end of b, which must be")
	fg.P("// at least m.ProtoSize() bytes long, and returns the number of bytes")
	fg.P("// written. It does not check required fields.")
	fg.P("func (m *", name, ") ProtoMarshalToSizedBuffer(b []byte) (int, error) {")
	fg.P("if m == nil {")
	fg.P("return 0, nil")
	fg.P("}")
	fg.P("i := len(b)")
	fg.P("if len(m.unknownFields) > 0 {")
	fg.P("i -= len(m.unknownFields)")
	fg.P("copy(b[i:], m.unknownFields)")
	fg.P("}")
	fg.buf.WriteString(body)
	fg.P("return len(b) - i, nil")
	fg.P("}")
	fg.P()
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
func (fg *fileGen) marshalSingular(f *desc.Field, v string) {
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
	if f.Kind == desc.KindString && f.ValidateUTF8 {
		fg.P("if !", fg.std("unicode/utf8"), ".ValidString(", v, ") {")
		fg.P("return 0, ", fg.errExpr(fg.owner(f.FullName)+"ErrUTF8", fmt.Sprintf(errInvalidUTF8F, f.FullName)))
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
			fg.marshalSingular(f.MapValue, "v")
			fg.marshalSingular(f.MapKey, "k")
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
		fg.marshalSingular(f, "v")
		fg.P("}")
	case f.Kind == desc.KindMessage, f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.marshalSingular(f, fv)
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.marshalSingular(f, "(*"+fv+")")
		fg.P("}")
	default:
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.marshalSingular(f, fv)
		fg.P("}")
	}
}

// --- Unmarshal ------------------------------------------------------------

func (fg *fileGen) genUnmarshal(mi *messageInfo) {
	name := mi.goName

	fg.P("// UnmarshalBinary replaces the contents of m with the decoded")
	fg.P("// wire-format message in b.")
	fg.P("func (m *", name, ") UnmarshalBinary(b []byte) error {")
	fg.P("*m = ", name, "{}")
	if mi.m.HasRequired() {
		fg.P("if err := m.ProtoMergeDepth(b, 0); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("return m.ProtoCheckInitialized()")
	} else {
		fg.P("return m.ProtoMergeDepth(b, 0)")
	}
	fg.P("}")
	fg.P()
	fg.P("// ProtoMerge decodes the wire-format message in b and merges it into m.")
	fg.P("// It does not check required fields.")
	fg.P("func (m *", name, ") ProtoMerge(b []byte) error { return m.ProtoMergeDepth(b, 0) }")
	fg.P()
	fg.P("// ProtoMergeDepth is ProtoMerge for a message nested depth levels deep.")
	fg.P("func (m *", name, ") ProtoMergeDepth(b []byte, depth int) error {")
	fg.P("if depth >= ", fg.c("MaxDepth"), " {")
	fg.P("goto errDepth")
	fg.P("}")
	fg.P("for len(b) > 0 {")
	fg.P("t, n := ", fg.std("encoding/binary"), ".Uvarint(b)")
	fg.emitCheckTag("t", "n")
	fg.P("start := b")
	fg.P("b = b[n:]")
	if len(mi.byNum) > 0 {
		fg.P("switch t {")
		for _, fi := range mi.byNum {
			fg.unmarshalField(fi)
		}
		fg.P("default:")
	}
	fg.P("// Unknown field, or a known field with an unexpected wire type.")
	fg.P("n, err := ", fg.fn("SkipField"), "(b, t, depth)")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("m.unknownFields = append(m.unknownFields, start[:len(start)-len(b)+n]...)")
	fg.P("b = b[n:]")
	if len(mi.byNum) > 0 {
		fg.P("}")
	}
	fg.P("}")
	fg.P("return nil")
	fg.P("errParse:")
	fg.P("return ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("errDepth:")
	fg.P("return ", fg.errExpr("ErrDepth", errDepthMsg))
	fg.P("}")
	fg.P()
}

// appendUnknownVarint emits code preserving raw varint x of field f as an
// unknown field; it is used for unrecognized closed-enum values.
func (fg *fileGen) appendUnknownVarint(f *desc.Field) {
	bin := fg.std("encoding/binary")
	fg.P("m.unknownFields = ", bin, ".AppendUvarint(", bin, ".AppendUvarint(m.unknownFields, ", fg.tagExpr(fg.fieldNum(f), wireVarint), "), x)")
}

func isClosedEnum(f *desc.Field) bool {
	return f.Kind == desc.KindEnum && f.EnumType.Closed
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
		// Obtain the target message, then merge into it.
		mv := "mv"
		target := func() {
			switch {
			case fi.oneof != nil:
				fg.P("var mv *", fg.msgType(f.MessageType))
				fg.P("if o, ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok && o.", fi.goName, " != nil {")
				fg.P("mv = o.", fi.goName)
				fg.P("} else {")
				fg.P("mv = &", fg.msgType(f.MessageType), "{}")
				fg.P("m.", fi.oneof.goName, " = &", fi.wrapper, "{", fi.goName, ": mv}")
				fg.P("}")
			case f.Repeated:
				fg.P("mv := &", fg.msgType(f.MessageType), "{}")
				fg.P(fv, " = append(", fv, ", mv)")
			default:
				fg.P("if ", fv, " == nil {")
				fg.P(fv, " = &", fg.msgType(f.MessageType), "{}")
				fg.P("}")
				mv = fv
			}
		}
		if f.Delimited {
			fg.P("case ", fg.tagExpr(num, wireStartGroup), ":")
			fg.P("n, err := ", fg.fn("SkipGroup"), "(b, ", num, ", depth)")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
			target()
			fg.P("if err := ", mv, ".ProtoMergeDepth(b[:n-", tagSize(f.Number, wireEndGroup), "], depth+1); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("b = b[n:]")
			return
		}
		fg.P("case ", fg.tagExpr(num, wireBytes), ":")
		fg.decBytes("v", "b")
		target()
		fg.P("if err := ", mv, ".ProtoMergeDepth(v, depth+1); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("b = b[n:]")
		return
	}

	if f.Repeated && f.Kind.Packable() {
		// Accept packed encoding regardless of the declared form.
		fg.P("case ", fg.tagExpr(num, wireBytes), ":")
		fg.decBytes("v", "b")
		fg.P("b = b[n:]")
		if fixedSize(f.Kind) > 1 {
			fs := fg.fixedSizeExpr(f.Kind)
			fg.P("if len(v)%", fs, " != 0 {")
			fg.P("goto errParse")
			fg.P("}")
			fg.P("if ", fv, " == nil {")
			fg.P(fv, " = make([]", fg.scalarGoType(f), ", 0, len(v)/", fs, ")")
			fg.P("}")
		}
		fg.P("for len(v) > 0 {")
		expr, n := fg.decodeScalar(f, "v")
		if isClosedEnum(f) {
			fg.P("v = v[", n, ":]")
			fg.P("if e := ", expr, "; !e.IsValid() {")
			fg.appendUnknownVarint(f)
			fg.P("} else {")
			fg.P(fv, " = append(", fv, ", e)")
			fg.P("}")
		} else {
			fg.P("v, ", fv, " = v[", n, ":], append(", fv, ", ", expr, ")")
		}
		fg.P("}")
	}

	fg.P("case ", fg.tagExpr(num, wireType(f)), ":")
	expr, n := fg.decodeScalar(f, "b")
	// Advance b in the same statement as the assignment, unless a closed
	// enum value must be checked first.
	adv := "b, "
	advExpr := "b[" + n + ":], "
	if isClosedEnum(f) {
		fg.P("b = b[", n, ":]")
		fg.P("e := ", expr)
		fg.P("if !e.IsValid() {")
		fg.appendUnknownVarint(f)
		fg.P("continue")
		fg.P("}")
		expr, adv, advExpr = "e", "", ""
	}
	switch {
	case fi.oneof != nil:
		fg.P(adv, "m.", fi.oneof.goName, " = ", advExpr, "&", fi.wrapper, "{", fi.goName, ": ", expr, "}")
	case f.Repeated:
		fg.P(adv, fv, " = ", advExpr, "append(", fv, ", ", expr, ")")
	case f.HasPresence && f.Kind != desc.KindBytes:
		fg.P(adv, fv, " = ", advExpr, "new(", expr, ")")
	default:
		fg.P(adv, fv, " = ", advExpr, expr)
	}
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
// field fi.
func (fg *fileGen) unmarshalMap(fi *fieldInfo) {
	f := fi.f
	key, val := f.MapKey, f.MapValue
	fg.P("case ", fg.tagExpr(fg.fieldNum(f), wireBytes), ":")
	fg.decBytes("v", "b")
	fg.P("mk, mv := ", fg.typedZero(key), ", ", fg.typedZero(val))
	fg.P("for len(v) > 0 {")
	fg.P("t, n := ", fg.std("encoding/binary"), ".Uvarint(v)")
	fg.emitCheckTag("t", "n")
	fg.P("v = v[n:]")
	fg.P("switch t {")
	fg.P("case ", fg.tagExpr(fg.fieldNum(key), wireType(key)), ":")
	kexpr, kn := fg.decodeScalar(key, "v")
	fg.P("v, mk = v[", kn, ":], ", kexpr)
	fg.P("case ", fg.tagExpr(fg.fieldNum(val), wireType(val)), ":")
	if val.Kind == desc.KindMessage {
		fg.decBytes("x", "v")
		fg.P("if mv == nil {")
		fg.P("mv = &", fg.msgType(val.MessageType), "{}")
		fg.P("}")
		fg.P("if err := mv.ProtoMergeDepth(x, depth+1); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("v = v[n:]")
	} else {
		vexpr, vn := fg.decodeScalar(val, "v")
		fg.P("v, mv = v[", vn, ":], ", vexpr)
	}
	fg.P("default:")
	fg.P("n, err := ", fg.fn("SkipField"), "(v, t, depth)")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("v = v[n:]")
	fg.P("}")
	fg.P("}")
	if val.Kind == desc.KindMessage {
		fg.P("if mv == nil {")
		fg.P("mv = &", fg.msgType(val.MessageType), "{}")
		fg.P("}")
	}
	if isClosedEnum(val) {
		fg.P("if !mv.IsValid() {")
		fg.P("m.unknownFields = append(m.unknownFields, start[:len(start)-len(b)+n]...)")
		fg.P("b = b[n:]")
		fg.P("continue")
		fg.P("}")
	}
	fg.P(fg.fn("MapSet"), "(&m.", fi.goName, ", mk, mv)")
	fg.P("b = b[n:]")
}

// --- Required fields --------------------------------------------------------

func (fg *fileGen) genCheckInitialized(mi *messageInfo) {
	fg.P("// ProtoCheckInitialized returns an error if any required field in m")
	fg.P("// or its sub-messages is not set.")
	if !mi.m.HasRequired() {
		fg.P("func (m *", mi.goName, ") ProtoCheckInitialized() error { return nil }")
		fg.P()
		return
	}
	fg.P("func (m *", mi.goName, ") ProtoCheckInitialized() error {")
	fg.P("if m == nil {")
	fg.P("return nil")
	fg.P("}")
	for _, fi := range mi.byNum {
		f := fi.f
		fv := "m." + fi.goName
		if f.Required {
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
			fg.P("if err := ", fv, ".ProtoCheckInitialized(); err != nil {")
			fg.P("return err")
			fg.P("}")
		}
	}
	fg.P("return nil")
	fg.P("}")
	fg.P()
}
