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
	if f.Kind == desc.KindMessage {
		fg.P("l := ", v, ".ProtoSize()")
		if f.Delimited {
			fg.P("n += ", 2*ts, " + l")
		} else {
			fg.P("n += ", ts, " + l + ", fg.sizeVarint("uint64(l)"))
		}
		return
	}
	fg.P("n += ", ts, " + ", fg.sizeExpr(f.Kind, v))
}

func (fg *fileGen) sizeField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	ts := tagSize(f.Number, wireType(f))
	switch {
	case f.IsMap:
		kts := tagSize(1, wireType(f.MapKey))
		vts := tagSize(2, wireType(f.MapValue))
		fg.P("for k, v := range ", fv, " {")
		fg.P("_, _ = k, v")
		if f.MapValue.Kind == desc.KindMessage {
			fg.P("lv := v.ProtoSize()")
			fg.P("l := ", kts, " + ", fg.sizeExpr(f.MapKey.Kind, "k"), " + ", vts, " + lv + ", fg.sizeVarint("uint64(lv)"))
		} else {
			fg.P("l := ", kts, " + ", fg.sizeExpr(f.MapKey.Kind, "k"), " + ", vts, " + ", fg.sizeExpr(f.MapValue.Kind, "v"))
		}
		fg.P("n += ", ts, " + l + ", fg.sizeVarint("uint64(l)"))
		fg.P("}")
	case f.Repeated && f.Packed:
		pts := tagSize(f.Number, wireBytes)
		fg.P("if len(", fv, ") > 0 {")
		if fs := fixedSize(f.Kind); fs == 1 {
			fg.P("l := len(", fv, ")")
		} else if fs > 0 {
			fg.P("l := len(", fv, ") * ", fg.fixedSizeExpr(f.Kind))
		} else {
			fg.P("l := 0")
			fg.P("for _, v := range ", fv, " {")
			fg.P("l += ", fg.sizeExpr(f.Kind, "v"))
			fg.P("}")
		}
		fg.P("n += ", pts, " + l + ", fg.sizeVarint("uint64(l)"))
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
	fg.P("func (m *", name, ") MarshalBinary() ([]byte, error) {")
	fg.P("return m.AppendBinary(nil)")
	fg.P("}")
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

// writeTag emits code writing the tag of field f with wire type wt
// backwards at b[:i].
func (fg *fileGen) writeTag(f *desc.Field, wt int) {
	fg.encVarint(fg.tagExpr(fg.fieldNum(f), wt))
}

// marshalSingular emits code writing present value v (with tag) of field f.
func (fg *fileGen) marshalSingular(f *desc.Field, v string) {
	if f.Kind == desc.KindMessage {
		if f.Delimited {
			fg.writeTag(f, wireEndGroup)
		}
		fg.P("n, err := ", v, ".ProtoMarshalToSizedBuffer(b[:i])")
		fg.P("if err != nil {")
		fg.P("return 0, err")
		fg.P("}")
		fg.P("i -= n")
		if f.Delimited {
			fg.writeTag(f, wireStartGroup)
		} else {
			fg.encVarint("uint64(n)")
			fg.writeTag(f, wireBytes)
		}
		return
	}
	if f.Kind == desc.KindString && f.ValidateUTF8 {
		fg.P("if !", fg.std("unicode/utf8"), ".ValidString(", v, ") {")
		fg.P("return 0, ", fg.errExpr(fg.owner(f.FullName)+"ErrUTF8", fmt.Sprintf(errInvalidUTF8F, f.FullName)))
		fg.P("}")
	}
	fg.encode(f.Kind, v)
	fg.writeTag(f, wireType(f))
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
			fg.encVarint("uint64(start - i)")
			fg.writeTag(f, wireBytes)
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
		fg.encode(f.Kind, "v")
		fg.P("}")
		fg.encVarint("uint64(start - i)")
		fg.writeTag(f, wireBytes)
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
	fg.P("func (m *", name, ") ProtoMerge(b []byte) error {")
	fg.P("return m.ProtoMergeDepth(b, 0)")
	fg.P("}")
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
				fg.P("mv := ", fv)
			}
		}
		if f.Delimited {
			fg.P("case ", fg.tagExpr(num, wireStartGroup), ":")
			fg.P("n, err := ", fg.fn("SkipGroup"), "(b, ", num, ", depth)")
			fg.P("if err != nil {")
			fg.P("return err")
			fg.P("}")
			target()
			fg.P("if err := mv.ProtoMergeDepth(b[:n-", tagSize(f.Number, wireEndGroup), "], depth+1); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("b = b[n:]")
			return
		}
		fg.P("case ", fg.tagExpr(num, wireBytes), ":")
		fg.decBytes("v", "b")
		target()
		fg.P("if err := mv.ProtoMergeDepth(v, depth+1); err != nil {")
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
		expr := fg.decodeScalar(f, "v")
		fg.P("v = v[n:]")
		if isClosedEnum(f) {
			fg.P("if e := ", expr, "; !e.IsValid() {")
			fg.appendUnknownVarint(f)
			fg.P("} else {")
			fg.P(fv, " = append(", fv, ", e)")
			fg.P("}")
		} else {
			fg.P(fv, " = append(", fv, ", ", expr, ")")
		}
		fg.P("}")
	}

	fg.P("case ", fg.tagExpr(num, wireType(f)), ":")
	expr := fg.decodeScalar(f, "b")
	fg.P("b = b[n:]")
	if isClosedEnum(f) {
		fg.P("e := ", expr)
		fg.P("if !e.IsValid() {")
		fg.appendUnknownVarint(f)
		fg.P("continue")
		fg.P("}")
		expr = "e"
	}
	switch {
	case fi.oneof != nil:
		fg.P("m.", fi.oneof.goName, " = &", fi.wrapper, "{", fi.goName, ": ", expr, "}")
	case f.Repeated:
		fg.P(fv, " = append(", fv, ", ", expr, ")")
	case f.HasPresence && f.Kind != desc.KindBytes:
		fg.P("v := ", expr)
		fg.P(fv, " = &v")
	default:
		fg.P(fv, " = ", expr)
	}
}

// unmarshalMap emits the ProtoMergeDepth case that decodes one entry of map
// field fi.
func (fg *fileGen) unmarshalMap(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	key, val := f.MapKey, f.MapValue
	fg.P("case ", fg.tagExpr(fg.fieldNum(f), wireBytes), ":")
	fg.decBytes("v", "b")
	fg.P("var mk ", fg.scalarGoType(key))
	fg.P("var mv ", fg.scalarGoType(val))
	fg.P("for len(v) > 0 {")
	fg.P("t, n := ", fg.std("encoding/binary"), ".Uvarint(v)")
	fg.emitCheckTag("t", "n")
	fg.P("v = v[n:]")
	fg.P("switch t {")
	fg.P("case ", fg.tagExpr(fg.fieldNum(key), wireType(key)), ":")
	kexpr := fg.decodeScalar(key, "v")
	fg.P("mk = ", kexpr)
	fg.P("v = v[n:]")
	fg.P("case ", fg.tagExpr(fg.fieldNum(val), wireType(val)), ":")
	if val.Kind == desc.KindMessage {
		fg.decBytes("x", "v")
		fg.P("if mv == nil {")
		fg.P("mv = &", fg.msgType(val.MessageType), "{}")
		fg.P("}")
		fg.P("if err := mv.ProtoMergeDepth(x, depth+1); err != nil {")
		fg.P("return err")
		fg.P("}")
	} else {
		vexpr := fg.decodeScalar(val, "v")
		fg.P("mv = ", vexpr)
	}
	fg.P("v = v[n:]")
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
	fg.P("if ", fv, " == nil {")
	fg.P(fv, " = make(", fg.fieldType(fi), ")")
	fg.P("}")
	fg.P(fv, "[mk] = mv")
	fg.P("b = b[n:]")
}

// --- Required fields --------------------------------------------------------

func (fg *fileGen) genCheckInitialized(mi *messageInfo) {
	fg.P("// ProtoCheckInitialized returns an error if any required field in m")
	fg.P("// or its sub-messages is not set.")
	fg.P("func (m *", mi.goName, ") ProtoCheckInitialized() error {")
	if !mi.m.HasRequired() {
		fg.P("return nil")
		fg.P("}")
		fg.P()
		return
	}
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
