// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"strconv"

	"github.com/canonical/cotorp/internal/desc"
)

func (fg *fileGen) genEnum(e *desc.Enum) {
	name := fg.g.enmNames[e]
	fg.comments(e.Comments, e.Deprecated)
	fg.P("type ", name, " int32")
	fg.P()
	fg.P("const (")
	for _, v := range e.Values {
		fg.comments(v.Comments, v.Deprecated)
		fg.P(fg.g.valNames[v], " ", name, " = ", v.Number)
	}
	fg.P(")")
	fg.P()
	fg.P("// ", name, "_name maps ", name, " numbers to their names.")
	fg.P("var ", name, "_name = map[int32]string{")
	seen := map[int32]bool{}
	var distinct []int32
	for _, v := range e.Values {
		if !seen[v.Number] {
			seen[v.Number] = true
			distinct = append(distinct, v.Number)
			fg.P(v.Number, ": ", strconv.Quote(v.Name), ",")
		}
	}
	fg.P("}")
	fg.P()
	fg.P("// ", name, "_value maps ", name, " names to their numbers.")
	fg.P("var ", name, "_value = map[string]int32{")
	for _, v := range e.Values {
		fg.P(strconv.Quote(v.Name), ": ", v.Number, ",")
	}
	fg.P("}")
	fg.P()
	fg.P("// Enum returns a pointer to a copy of x.")
	fg.P("func (x ", name, ") Enum() *", name, " { return &x }")
	fg.P()
	fg.P("// String returns the name of x, or its number if it has no name.")
	fg.P("func (x ", name, ") String() string {")
	fg.P("if s, ok := ", name, "_name[int32(x)]; ok {")
	fg.P("return s")
	fg.P("}")
	fg.P("return ", fg.std("strconv"), ".Itoa(int(x))")
	fg.P("}")
	fg.P()
	fg.P("// IsValid reports whether x is a declared value of ", name, ".")
	fg.P("func (x ", name, ") IsValid() bool {")
	fg.P("switch x {")
	cases := ""
	for i, n := range distinct {
		if i > 0 {
			cases += ", "
		}
		cases += strconv.Itoa(int(n))
	}
	fg.P("case ", cases, ":")
	fg.P("return true")
	fg.P("}")
	fg.P("return false")
	fg.P("}")
	fg.P()
}

// fieldType returns the Go type of a struct field (or oneof wrapper field).
func (fg *fileGen) fieldType(fi *fieldInfo) string {
	f := fi.f
	if f.IsMap {
		return "map[" + fg.scalarGoType(f.MapKey) + "]" + fg.scalarGoType(f.MapValue)
	}
	t := fg.scalarGoType(f)
	switch {
	case f.Repeated:
		return "[]" + t
	case fi.oneof != nil:
		return t
	case f.HasPresence && f.Kind != desc.KindMessage && f.Kind != desc.KindBytes:
		return "*" + t
	}
	return t
}

func (fg *fileGen) genMessage(mi *messageInfo) {
	m := mi.m
	name := mi.goName

	// Struct.
	fg.comments(m.Comments, m.Deprecated)
	fg.P("type ", name, " struct {")
	emitted := map[*oneofInfo]bool{}
	for _, fi := range mi.fields {
		if oi := fi.oneof; oi != nil {
			if emitted[oi] {
				continue
			}
			emitted[oi] = true
			fg.comments(oi.o.Comments, false)
			if oi.o.Comments != "" {
				fg.P("//")
			}
			fg.P("// Types that are valid to be assigned to ", oi.goName, ":")
			fg.P("//")
			for _, of := range oi.fields {
				fg.P("//	*", of.wrapper)
			}
			fg.P(oi.goName, " ", oi.iface)
			continue
		}
		fg.comments(fi.f.Comments, fi.f.Deprecated)
		fg.P(fi.goName, " ", fg.fieldType(fi))
	}
	fg.P()
	fg.P("unknownFields []byte")
	fg.P("}")
	fg.P()

	// Default values.
	fg.genDefaults(mi)

	fg.P("// Reset clears all fields of m.")
	fg.P("func (m *", name, ") Reset() { *m = ", name, "{} }")
	fg.P()

	// Getters.
	for _, fi := range mi.fields {
		if fi.oneof != nil && fi.oneof.fields[0] == fi {
			oi := fi.oneof
			fg.P("func (m *", name, ") ", oi.getter, "() ", oi.iface, " {")
			fg.P("if m != nil {")
			fg.P("return m.", oi.goName)
			fg.P("}")
			fg.P("return nil")
			fg.P("}")
			fg.P()
		}
		fg.genGetter(mi, fi)
	}

	fg.P("// ProtoUnknownFields returns the raw bytes of fields that were not")
	fg.P("// recognized when m was decoded.")
	fg.P("func (m *", name, ") ProtoUnknownFields() []byte {")
	fg.P("if m == nil {")
	fg.P("return nil")
	fg.P("}")
	fg.P("return m.unknownFields")
	fg.P("}")
	fg.P()

	// Oneof types.
	for _, oi := range mi.oneofs {
		fg.P("type ", oi.iface, " interface {")
		fg.P(oi.iface, "()")
		fg.P("}")
		fg.P()
		for _, fi := range oi.fields {
			fg.comments(fi.f.Comments, fi.f.Deprecated)
			fg.P("type ", fi.wrapper, " struct {")
			fg.P(fi.goName, " ", fg.fieldType(fi))
			fg.P("}")
			fg.P()
			fg.P("func (*", fi.wrapper, ") ", oi.iface, "() {}")
			fg.P()
		}
	}

	fg.genSize(mi)
	fg.genMarshal(mi)
	fg.genUnmarshal(mi)
	fg.genCheckInitialized(mi)
	fg.genJSON(mi)
}

func (fg *fileGen) genDefaults(mi *messageInfo) {
	var consts, vars [][2]string
	for _, fi := range mi.fields {
		f := fi.f
		dv := f.Default
		if dv == nil {
			continue
		}
		typ := fg.scalarGoType(f)
		switch f.Kind {
		case desc.KindInt32, desc.KindInt64, desc.KindSint32, desc.KindSint64, desc.KindSfixed32, desc.KindSfixed64:
			consts = append(consts, [2]string{fi.defName, typ + " = " + strconv.FormatInt(dv.Int, 10)})
		case desc.KindUint32, desc.KindUint64, desc.KindFixed32, desc.KindFixed64:
			consts = append(consts, [2]string{fi.defName, typ + " = " + strconv.FormatUint(dv.Uint, 10)})
		case desc.KindFloat, desc.KindDouble:
			bits := 64
			if f.Kind == desc.KindFloat {
				bits = 32
			}
			if lit, ok := floatLiteral(dv.Float, bits); ok {
				consts = append(consts, [2]string{fi.defName, typ + " = " + lit})
			} else {
				vars = append(vars, [2]string{fi.defName, typ + " = " + typ + "(" + fg.floatSpecialExpr(dv.Float) + ")"})
			}
		case desc.KindBool:
			consts = append(consts, [2]string{fi.defName, typ + " = " + strconv.FormatBool(dv.Bool)})
		case desc.KindString:
			consts = append(consts, [2]string{fi.defName, typ + " = " + strconv.Quote(dv.String)})
		case desc.KindBytes:
			vars = append(vars, [2]string{fi.defName, "= []byte(" + strconv.Quote(dv.String) + ")"})
		case desc.KindEnum:
			consts = append(consts, [2]string{fi.defName, typ + " = " + fg.enumValue(dv.Enum)})
		}
	}
	if len(consts) > 0 {
		fg.P("// Default values for ", mi.goName, " fields.")
		fg.P("const (")
		for _, c := range consts {
			fg.P(c[0], " ", c[1])
		}
		fg.P(")")
		fg.P()
	}
	if len(vars) > 0 {
		fg.P("// Default values for ", mi.goName, " fields.")
		fg.P("var (")
		for _, c := range vars {
			fg.P(c[0], " ", c[1])
		}
		fg.P(")")
		fg.P()
	}
}

// defaultExpr is the value a getter returns for an unset field.
func (fg *fileGen) defaultExpr(fi *fieldInfo) string {
	if fi.defName != "" {
		if fi.f.Kind == desc.KindBytes {
			return "append([]byte(nil), " + fi.defName + "...)"
		}
		return fi.defName
	}
	return fg.zeroExpr(fi.f)
}

func (fg *fileGen) genGetter(mi *messageInfo, fi *fieldInfo) {
	f := fi.f
	typ := fg.fieldType(fi)
	fg.P("func (m *", mi.goName, ") ", fi.getter, "() ", func() string {
		if fi.oneof == nil && f.HasPresence && f.Kind != desc.KindMessage && f.Kind != desc.KindBytes {
			return typ[1:] // strip pointer
		}
		return typ
	}(), " {")
	switch {
	case fi.oneof != nil:
		fg.P("if x, ok := m.", fi.oneof.getter, "().(*", fi.wrapper, "); ok {")
		fg.P("return x.", fi.goName)
		fg.P("}")
		fg.P("return ", fg.defaultExpr(fi))
	case f.Repeated || f.Kind == desc.KindMessage:
		fg.P("if m != nil {")
		fg.P("return m.", fi.goName)
		fg.P("}")
		fg.P("return nil")
	case f.HasPresence && f.Kind == desc.KindBytes:
		fg.P("if m != nil && m.", fi.goName, " != nil {")
		fg.P("return m.", fi.goName)
		fg.P("}")
		fg.P("return ", fg.defaultExpr(fi))
	case f.HasPresence:
		fg.P("if m != nil && m.", fi.goName, " != nil {")
		fg.P("return *m.", fi.goName)
		fg.P("}")
		fg.P("return ", fg.defaultExpr(fi))
	default:
		fg.P("if m != nil {")
		fg.P("return m.", fi.goName)
		fg.P("}")
		fg.P("return ", fg.zeroExpr(f))
	}
	fg.P("}")
	fg.P()
}
