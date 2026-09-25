// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"fmt"

	"github.com/canonical/cotorp/internal/desc"
)

// This file generates the wire-format methods. All encoding and decoding
// logic is emitted inline; the only calls are to the standard library and
// to the corresponding methods of other generated messages.

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
		fg.P("{")
		fg.P("l := ", v, ".ProtoSize()")
		if f.Delimited {
			fg.P("n += ", 2*ts, " + l")
		} else {
			fg.P("n += ", ts, " + l + ", fg.sizeVarint("uint64(l)"))
		}
		fg.P("}")
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
		if fs := fixedSize(f.Kind); fs > 0 {
			fg.P("l := len(", fv, ") * ", fs)
		} else {
			fg.P("l := 0")
			fg.P("for _, v := range ", fv, " {")
			fg.P("l += ", fg.sizeExpr(f.Kind, "v"))
			fg.P("}")
		}
		fg.P("n += ", pts, " + l + ", fg.sizeVarint("uint64(l)"))
		fg.P("}")
	case f.Repeated:
		if fs := fixedSize(f.Kind); fs > 0 {
			fg.P("n += len(", fv, ") * ", ts+fs)
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
	fg.P("l := len(b)")
	fg.P("b = ", fg.std("slices"), ".Grow(b, size)[:l+size]")
	fg.P("n, err := m.ProtoMarshalToSizedBuffer(b[l:])")
	fg.P("if err != nil {")
	fg.P("return b[:l], err")
	fg.P("}")
	fg.P("if n != size {")
	fg.P("return b[:l], ", fg.errExpr(errSizeMsg))
	fg.P("}")
	fg.P("return b, nil")
	fg.P("}")
	fg.P()

	fg.usesU = false
	body := fg.capture(func() {
		for j := len(mi.byNum) - 1; j >= 0; j-- {
			fi := mi.byNum[j]
			if fi.oneof != nil {
				fg.P("if o, ok := m.", fi.oneof.goName, ".(*", fi.wrapper, "); ok {")
				fg.marshalSingular(fi.f, "o."+fi.goName, fi.f.Number)
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
	if fg.usesU {
		fg.P("var u uint64")
	}
	fg.P("if len(m.unknownFields) > 0 {")
	fg.P("i -= len(m.unknownFields)")
	fg.P("copy(b[i:], m.unknownFields)")
	fg.P("}")
	fg.buf.WriteString(body)
	fg.P("return len(b) - i, nil")
	fg.P("}")
	fg.P()
}

func (fg *fileGen) writeTag(num int32, wt int) {
	tb := tagBytes(num, wt)
	if len(tb) == 1 {
		fg.P("i--")
		fg.P(fmt.Sprintf("b[i] = 0x%02x", tb[0]))
		return
	}
	fg.P("i -= ", len(tb))
	for j, c := range tb {
		if j == 0 {
			fg.P(fmt.Sprintf("b[i] = 0x%02x", c))
		} else {
			fg.P(fmt.Sprintf("b[i+%d] = 0x%02x", j, c))
		}
	}
}

// marshalSingular emits code writing present value v (with tag) for field
// number num.
func (fg *fileGen) marshalSingular(f *desc.Field, v string, num int32) {
	if f.Kind == desc.KindMessage {
		fg.P("{")
		if f.Delimited {
			fg.writeTag(num, wireEndGroup)
		}
		fg.P("n, err := ", v, ".ProtoMarshalToSizedBuffer(b[:i])")
		fg.P("if err != nil {")
		fg.P("return 0, err")
		fg.P("}")
		fg.P("i -= n")
		if f.Delimited {
			fg.writeTag(num, wireStartGroup)
		} else {
			fg.encVarint("uint64(n)")
			fg.writeTag(num, wireBytes)
		}
		fg.P("}")
		return
	}
	if f.Kind == desc.KindString && f.ValidateUTF8 {
		fg.P("if !", fg.std("unicode/utf8"), ".ValidString(", v, ") {")
		fg.P("return 0, ", fg.errExpr(fmt.Sprintf(errInvalidUTF8F, f.FullName)))
		fg.P("}")
	}
	fg.encode(f.Kind, v)
	fg.writeTag(num, wireType(f))
}

func (fg *fileGen) marshalField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	switch {
	case f.IsMap:
		entry := func() {
			fg.P("start := i")
			fg.marshalSingular(f.MapValue, "v", 2)
			fg.marshalSingular(f.MapKey, "k", 1)
			fg.encVarint("uint64(start - i)")
			fg.writeTag(f.Number, wireBytes)
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
			fg.P("keys := make([]", fg.scalarGoType(f.MapKey), ", 0, len(", fv, "))")
			fg.P("for k := range ", fv, " {")
			fg.P("keys = append(keys, k)")
			fg.P("}")
			fg.P(fg.std("slices"), ".Sort(keys)")
			fg.P("for j := len(keys) - 1; j >= 0; j-- {")
			fg.P("k := keys[j]")
			fg.P("v := ", fv, "[k]")
			entry()
			fg.P("}")
		}
		fg.P("}")
	case f.Repeated && f.Packed:
		fg.P("if len(", fv, ") > 0 {")
		fg.P("start := i")
		fg.P("for j := len(", fv, ") - 1; j >= 0; j-- {")
		fg.encode(f.Kind, fv+"[j]")
		fg.P("}")
		fg.encVarint("uint64(start - i)")
		fg.writeTag(f.Number, wireBytes)
		fg.P("}")
	case f.Repeated:
		fg.P("for j := len(", fv, ") - 1; j >= 0; j-- {")
		fg.marshalSingular(f, fv+"[j]", f.Number)
		fg.P("}")
	case f.Kind == desc.KindMessage, f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if ", fv, " != nil {")
		fg.marshalSingular(f, fv, f.Number)
		fg.P("}")
	case f.HasPresence:
		fg.P("if ", fv, " != nil {")
		fg.marshalSingular(f, "(*"+fv+")", f.Number)
		fg.P("}")
	default:
		fg.P("if ", fg.nonZeroExpr(f.Kind, fv), " {")
		fg.marshalSingular(f, fv, f.Number)
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
	fg.P("if depth >= ", maxDepth, " {")
	fg.P("goto errDepth")
	fg.P("}")
	fg.P("for len(b) > 0 {")
	fg.decodeTag("b")
	fg.P("start := b")
	fg.P("b = b[n:]")
	if len(mi.byNum) > 0 {
		fg.P("switch num {")
		for _, fi := range mi.byNum {
			fg.P("case ", fi.f.Number, ":")
			fg.unmarshalField(fi)
		}
		fg.P("}")
	}
	fg.P("// Unknown field, or a known field with an unexpected wire type.")
	fg.emitSkip("b")
	fg.P("m.unknownFields = append(m.unknownFields, start[:len(start)-len(b)+n]...)")
	fg.P("b = b[n:]")
	fg.P("}")
	fg.P("return nil")
	fg.P("errParse:")
	fg.P("return ", fg.errExpr(errParseMsg))
	fg.P("errDepth:")
	fg.P("return ", fg.errExpr(errDepthMsg))
	fg.P("}")
	fg.P()
}

// decodeTag emits code decoding a tag from buf into num, typ and n.
func (fg *fileGen) decodeTag(buf string) {
	fg.P("t, n := ", fg.std("encoding/binary"), ".Uvarint(", buf, ")")
	fg.P("if n <= 0 || t>>3 == 0 || t>>3 > ", desc.MaxFieldNumber, " {")
	fg.P("goto errParse")
	fg.P("}")
	fg.P("num, typ := int32(t>>3), int(t&7)")
}

// appendUnknownVarint emits code preserving raw varint x of field num as an
// unknown field; it is used for unrecognized closed-enum values.
func (fg *fileGen) appendUnknownVarint(num int32) {
	fg.P("m.unknownFields = ", fg.std("encoding/binary"), ".AppendUvarint(append(m.unknownFields, ", byteList(tagBytes(num, wireVarint)), "), x)")
}

func isClosedEnum(f *desc.Field) bool {
	return f.Kind == desc.KindEnum && f.EnumType.Closed
}

func (fg *fileGen) unmarshalField(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName

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
			fg.P("if typ == ", wireStartGroup, " {")
			fg.P("var n int")
			fg.emitSkipGroup("b")
			target()
			fg.P("if err := mv.ProtoMergeDepth(b[:n-", tagSize(f.Number, wireEndGroup), "], depth+1); err != nil {")
			fg.P("return err")
			fg.P("}")
			fg.P("b = b[n:]")
			fg.P("continue")
			fg.P("}")
			return
		}
		fg.P("if typ == ", wireBytes, " {")
		fg.decBytes("v", "b")
		target()
		fg.P("if err := mv.ProtoMergeDepth(v, depth+1); err != nil {")
		fg.P("return err")
		fg.P("}")
		fg.P("b = b[n:]")
		fg.P("continue")
		fg.P("}")
		return
	}

	if f.Repeated && f.Kind.Packable() {
		// Accept packed encoding regardless of the declared form.
		fg.P("if typ == ", wireBytes, " {")
		fg.decBytes("v", "b")
		fg.P("b = b[n:]")
		if fs := fixedSize(f.Kind); fs > 1 {
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
			fg.appendUnknownVarint(f.Number)
			fg.P("} else {")
			fg.P(fv, " = append(", fv, ", e)")
			fg.P("}")
		} else {
			fg.P(fv, " = append(", fv, ", ", expr, ")")
		}
		fg.P("}")
		fg.P("continue")
		fg.P("}")
	}

	fg.P("if typ == ", wireType(f), " {")
	expr := fg.decodeScalar(f, "b")
	fg.P("b = b[n:]")
	if isClosedEnum(f) {
		fg.P("e := ", expr)
		fg.P("if !e.IsValid() {")
		fg.appendUnknownVarint(f.Number)
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
	fg.P("continue")
	fg.P("}")
}

func (fg *fileGen) unmarshalMap(fi *fieldInfo) {
	f := fi.f
	fv := "m." + fi.goName
	key, val := f.MapKey, f.MapValue
	fg.P("if typ == ", wireBytes, " {")
	fg.decBytes("v", "b")
	fg.P("var mk ", fg.scalarGoType(key))
	fg.P("var mv ", fg.scalarGoType(val))
	fg.P("for len(v) > 0 {")
	fg.decodeTag("v")
	fg.P("v = v[n:]")
	// Key.
	fg.P("if num == 1 && typ == ", wireType(key), " {")
	kexpr := fg.decodeScalar(key, "v")
	fg.P("mk = ", kexpr)
	fg.P("v = v[n:]")
	fg.P("continue")
	fg.P("}")
	// Value.
	fg.P("if num == 2 && typ == ", wireType(val), " {")
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
	fg.P("continue")
	fg.P("}")
	fg.emitSkip("v")
	fg.P("v = v[n:]")
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
	fg.P("continue")
	fg.P("}")
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
			fg.P("return ", fg.errExpr(fmt.Sprintf(errRequiredFmt, f.FullName)))
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
