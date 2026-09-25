// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package desc

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/canonical/cotorp/internal/parser"
)

type symbolKind int

const (
	symPackage symbolKind = iota
	symMessage
	symEnum
	symEnumValue
	symField
	symOneof
)

func (k symbolKind) String() string {
	switch k {
	case symPackage:
		return "package"
	case symMessage:
		return "message"
	case symEnum:
		return "enum"
	case symEnumValue:
		return "enum value"
	case symField:
		return "field"
	default:
		return "oneof"
	}
}

type symbol struct {
	kind symbolKind
	pos  parser.Pos
	file *File
	msg  *Message
	enum *Enum
}

func (s symbol) isType() bool { return s.kind == symMessage || s.kind == symEnum }

// MaxFieldNumber is the largest valid field number.
const MaxFieldNumber = 1<<29 - 1

const maxErrors = 20

type builder struct {
	f       *File
	visible []*File
	errs    []error
}

// Build links and validates a parsed file. imports maps each import path
// named by the file to its already-built File.
func Build(ast *parser.File, imports map[string]*File) (*File, error) {
	b := &builder{f: &File{
		Path:    ast.Name,
		AST:     ast,
		Package: ast.Package,
		symbols: map[string]symbol{},
	}}
	b.build(imports)
	if len(b.errs) > 0 {
		return nil, errors.Join(b.errs...)
	}
	return b.f, nil
}

func (b *builder) errorf(pos parser.Pos, format string, args ...any) {
	if len(b.errs) < maxErrors {
		b.errs = append(b.errs, parser.Errorf(pos, format, args...))
	}
}

func (b *builder) build(imports map[string]*File) {
	f, ast := b.f, b.f.AST
	switch ast.Syntax {
	case "proto2":
		f.Edition = EditionProto2
	case "proto3":
		f.Edition = EditionProto3
	case "editions":
		switch ast.Edition {
		case "2023":
			f.Edition = Edition2023
		case "2024":
			f.Edition = Edition2024
		default:
			b.errorf(ast.SyntaxPos, "edition %q is not supported (supported: 2023, 2024)", ast.Edition)
			return
		}
	}

	// Imports.
	seen := map[string]bool{}
	for _, imp := range ast.Imports {
		if seen[imp.Path] {
			b.errorf(imp.Pos, "import %q was listed twice", imp.Path)
			continue
		}
		seen[imp.Path] = true
		if imp.Option {
			// "import option" dependencies only provide option
			// definitions; they are not visible for type resolution.
			continue
		}
		dep := imports[imp.Path]
		if dep == nil {
			b.errorf(imp.Pos, "import %q not found", imp.Path)
			continue
		}
		f.Imports = append(f.Imports, dep)
		if imp.Public {
			f.PublicImports = append(f.PublicImports, dep)
		}
	}
	b.visible = []*File{f}
	visited := map[*File]bool{f: true}
	var addPublic func(*File)
	addPublic = func(d *File) {
		if visited[d] {
			return
		}
		visited[d] = true
		b.visible = append(b.visible, d)
		for _, p := range d.PublicImports {
			addPublic(p)
		}
	}
	for _, d := range f.Imports {
		addPublic(d)
	}

	// File options.
	b.checkOptionNames(ast.Options, fileOptions, "file")
	f.Features = b.applyFeatures(defaultFeatures(f.Edition), ast.Options, "file")
	for _, o := range ast.Options {
		switch o.SimpleName() {
		case "go_package":
			if o.Value.Kind != parser.ValueString {
				b.errorf(o.Pos, "go_package must be a string")
				continue
			}
			f.GoPackage = o.Value.Str
		}
	}

	// Declare package symbols.
	if f.Package != "" {
		parts := strings.Split(f.Package, ".")
		for i := range parts {
			b.declare(strings.Join(parts[:i+1], "."), symbol{kind: symPackage, pos: ast.PackagePos, file: f})
		}
	}

	// Declare types.
	for _, am := range ast.Messages {
		f.Messages = append(f.Messages, b.declareMessage(am, nil, f.Package, f.Features))
	}
	for _, ae := range ast.Enums {
		f.Enums = append(f.Enums, b.declareEnum(ae, nil, f.Package, f.Features))
	}
	for _, ext := range ast.Extends {
		b.declareExtendGroups(ext, nil, f.Package, f.Features)
	}
	if len(b.errs) > 0 {
		return
	}

	// Resolve fields.
	for _, m := range f.AllMessages {
		b.buildMessageFields(m)
	}
	for _, ext := range ast.Extends {
		f.Extensions = append(f.Extensions, b.buildExtensions(ext, nil, f.Package, f.Features)...)
	}
	for _, m := range f.AllMessages {
		for _, ext := range m.AST.Extends {
			m.Extensions = append(m.Extensions, b.buildExtensions(ext, m, m.FullName, m.Features)...)
		}
	}
	if len(b.errs) > 0 {
		return
	}
	for _, m := range f.AllMessages {
		b.validateMessage(m)
	}
	for _, e := range f.AllEnums {
		b.validateEnum(e)
	}
}

func join(scope, name string) string {
	if scope == "" {
		return name
	}
	return scope + "." + name
}

func (b *builder) lookup(name string) (symbol, bool) {
	for _, vf := range b.visible {
		if s, ok := vf.symbols[name]; ok {
			return s, true
		}
	}
	return symbol{}, false
}

func (b *builder) declare(name string, s symbol) {
	if existing, ok := b.lookup(name); ok {
		if s.kind == symPackage && existing.kind == symPackage {
			return
		}
		where := ""
		if existing.file != b.f {
			where = fmt.Sprintf(" in %q", existing.file.Path)
		}
		if s.kind == symEnumValue && existing.kind == symEnumValue {
			b.errorf(s.pos, "%q is already defined%s; note that enum values use C++ scoping rules, meaning that enum values are siblings of their type, not children of it", name, where)
			return
		}
		b.errorf(s.pos, "%q is already defined as a %s%s", name, existing.kind, where)
		return
	}
	b.f.symbols[name] = s
}

// resolveType resolves a type reference relative to scope using protobuf's
// scoping rules.
func (b *builder) resolveType(name, scope string, pos parser.Pos) (symbol, bool) {
	if strings.HasPrefix(name, ".") {
		if s, ok := b.lookup(name[1:]); ok && s.isType() {
			return s, true
		}
		b.errorf(pos, "%q is not defined", name)
		return symbol{}, false
	}
	first, rest, _ := strings.Cut(name, ".")
	for sc := scope; ; {
		if s, ok := b.lookup(join(sc, first)); ok {
			if rest == "" {
				if s.isType() {
					return s, true
				}
			} else if s.kind == symPackage || s.kind == symMessage {
				full := join(sc, name)
				if s2, ok := b.lookup(full); ok && s2.isType() {
					return s2, true
				}
				b.errorf(pos, "%q is resolved to %q, which is not defined; the innermost scope is searched first in name resolution, consider using a leading '.' (i.e., \".%s\") to start from the outermost scope", name, full, name)
				return symbol{}, false
			}
		}
		if sc == "" {
			break
		}
		if i := strings.LastIndexByte(sc, '.'); i >= 0 {
			sc = sc[:i]
		} else {
			sc = ""
		}
	}
	b.errorf(pos, "%q is not defined", name)
	return symbol{}, false
}

func (b *builder) declareMessage(am *parser.Message, parent *Message, scope string, parentFeatures Features) *Message {
	m := &Message{
		Name:     am.Name,
		FullName: join(scope, am.Name),
		File:     b.f,
		Parent:   parent,
		Comments: am.Comments,
		AST:      am,
	}
	b.declare(m.FullName, symbol{kind: symMessage, pos: am.Pos, file: b.f, msg: m})
	b.f.AllMessages = append(b.f.AllMessages, m)

	b.checkOptionNames(am.Options, messageOptions, "message")
	m.Features = b.applyFeatures(parentFeatures, am.Options, "message")
	for _, o := range am.Options {
		switch o.SimpleName() {
		case "deprecated":
			m.Deprecated = b.boolOption(o)
		case "map_entry":
			b.errorf(o.Pos, "map_entry should not be set explicitly; use map<KeyType, ValueType> instead")
		case "message_set_wire_format":
			// MessageSet messages only carry extensions, which are
			// preserved verbatim as unknown fields.
			if b.boolOption(o) && len(am.Fields) > 0 {
				b.errorf(o.Pos, "messages with message_set_wire_format cannot have fields, only extensions")
			}
		}
	}
	messageSet := false
	for _, o := range am.Options {
		if o.SimpleName() == "message_set_wire_format" && o.Value.Kind == parser.ValueIdent && o.Value.Ident == "true" {
			messageSet = true
		}
	}
	for _, r := range am.ExtensionRanges {
		if r.Max && messageSet {
			r.End = math.MaxInt32
		}
		m.ExtensionRanges = append(m.ExtensionRanges, [2]int32{int32(r.Start), int32(r.End)})
	}

	for _, af := range am.Fields {
		b.declare(join(m.FullName, af.Name), symbol{kind: symField, pos: af.Pos, file: b.f})
		switch {
		case af.Group != nil:
			m.Messages = append(m.Messages, b.declareMessage(af.Group, m, m.FullName, m.Features))
		case af.IsMap:
			entry := &Message{
				Name:       mapEntryName(af.Name),
				File:       b.f,
				Parent:     m,
				IsMapEntry: true,
				AST:        &parser.Message{Pos: af.Pos, Name: mapEntryName(af.Name)},
			}
			entry.FullName = join(m.FullName, entry.Name)
			b.declare(entry.FullName, symbol{kind: symMessage, pos: af.Pos, file: b.f, msg: entry})
			b.f.AllMessages = append(b.f.AllMessages, entry)
			m.Messages = append(m.Messages, entry)
		}
	}
	for _, ao := range am.Oneofs {
		b.declare(join(m.FullName, ao.Name), symbol{kind: symOneof, pos: ao.Pos, file: b.f})
	}
	for _, nm := range am.Messages {
		m.Messages = append(m.Messages, b.declareMessage(nm, m, m.FullName, m.Features))
	}
	for _, ne := range am.Enums {
		m.Enums = append(m.Enums, b.declareEnum(ne, m, m.FullName, m.Features))
	}
	for _, ext := range am.Extends {
		b.declareExtendGroups(ext, m, m.FullName, m.Features)
	}
	return m
}

// declareExtendGroups declares the message types of group extensions, which
// live in the scope enclosing the extend block.
func (b *builder) declareExtendGroups(ext *parser.Extend, parent *Message, scope string, features Features) {
	for _, af := range ext.Fields {
		if af.Group == nil {
			continue
		}
		gm := b.declareMessage(af.Group, parent, scope, features)
		if parent != nil {
			parent.Messages = append(parent.Messages, gm)
		} else {
			b.f.Messages = append(b.f.Messages, gm)
		}
	}
}

// mapEntryName returns the synthesized entry message name for a map field,
// matching protoc: "foo_bar" -> "FooBarEntry".
func mapEntryName(field string) string {
	var sb strings.Builder
	upper := true
	for i := 0; i < len(field); i++ {
		c := field[i]
		if c == '_' {
			upper = true
			continue
		}
		if upper && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		upper = false
		sb.WriteByte(c)
	}
	sb.WriteString("Entry")
	return sb.String()
}

func (b *builder) declareEnum(ae *parser.Enum, parent *Message, scope string, parentFeatures Features) *Enum {
	e := &Enum{
		Name:     ae.Name,
		FullName: join(scope, ae.Name),
		File:     b.f,
		Parent:   parent,
		Comments: ae.Comments,
		AST:      ae,
	}
	b.declare(e.FullName, symbol{kind: symEnum, pos: ae.Pos, file: b.f, enum: e})
	b.f.AllEnums = append(b.f.AllEnums, e)
	b.checkOptionNames(ae.Options, enumOptions, "enum")
	e.Features = b.applyFeatures(parentFeatures, ae.Options, "enum")
	e.Closed = e.Features.EnumType == EnumClosed
	for _, o := range ae.Options {
		if o.SimpleName() == "deprecated" {
			e.Deprecated = b.boolOption(o)
		}
	}
	for _, av := range ae.Values {
		v := &EnumValue{
			Name:     av.Name,
			FullName: join(scope, av.Name),
			Number:   av.Number,
			Enum:     e,
			Comments: av.Comments,
		}
		b.checkOptionNames(av.Options, enumValueOptions, "enum value")
		b.applyFeatures(e.Features, av.Options, "enum_entry")
		for _, o := range av.Options {
			if o.SimpleName() == "deprecated" {
				v.Deprecated = b.boolOption(o)
			}
		}
		b.declare(v.FullName, symbol{kind: symEnumValue, pos: av.Pos, file: b.f})
		e.Values = append(e.Values, v)
	}
	return e
}

func (b *builder) boolOption(o *parser.Option) bool {
	if o.Value.Kind == parser.ValueIdent && !o.Value.Neg {
		switch o.Value.Ident {
		case "true":
			return true
		case "false":
			return false
		}
	}
	b.errorf(o.Pos, "option %s must be true or false", optionName(o))
	return false
}

func optionName(o *parser.Option) string {
	var parts []string
	for _, p := range o.Name {
		if p.Ext {
			parts = append(parts, "("+p.Name+")")
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, ".")
}

func (b *builder) buildMessageFields(m *Message) {
	if m.IsMapEntry {
		// Built together with the map field.
		return
	}
	am := m.AST
	oneofs := map[*parser.Oneof]*Oneof{}
	oneofFeatures := map[*parser.Oneof]Features{}
	for _, ao := range am.Oneofs {
		o := &Oneof{Name: ao.Name, FullName: join(m.FullName, ao.Name), Message: m, Comments: ao.Comments}
		b.checkOptionNames(ao.Options, oneofOptions, "oneof")
		oneofFeatures[ao] = b.applyFeatures(m.Features, ao.Options, "oneof")
		oneofs[ao] = o
		m.Oneofs = append(m.Oneofs, o)
		if len(ao.Fields) == 0 {
			b.errorf(ao.Pos, "oneof %q must have at least one field", ao.Name)
		}
	}
	for _, af := range am.Fields {
		base := m.Features
		if af.Oneof != nil {
			base = oneofFeatures[af.Oneof]
		}
		fd := b.buildField(af, m, m, m.FullName, base, false)
		if fd == nil {
			continue
		}
		if af.Oneof != nil {
			o := oneofs[af.Oneof]
			fd.Oneof = o
			o.Fields = append(o.Fields, fd)
		}
		m.Fields = append(m.Fields, fd)
	}
}

func (b *builder) buildExtensions(ext *parser.Extend, parent *Message, scope string, features Features) []*Field {
	s, ok := b.resolveType(ext.Extendee, scope, ext.Pos)
	if !ok {
		return nil
	}
	if s.kind != symMessage {
		b.errorf(ext.Pos, "%q is not a message type", ext.Extendee)
		return nil
	}
	var out []*Field
	for _, af := range ext.Fields {
		fd := b.buildField(af, parent, nil, scope, features, true)
		if fd == nil {
			continue
		}
		fd.Extendee = s.msg
		if af.IsMap {
			b.errorf(af.Pos, "map fields are not allowed in extensions")
		}
		inRange := false
		for _, r := range s.msg.ExtensionRanges {
			if fd.Number >= r[0] && fd.Number <= r[1] {
				inRange = true
			}
		}
		if !inRange {
			b.errorf(af.Pos, "%q does not declare %d as an extension number", s.msg.FullName, fd.Number)
		}
		out = append(out, fd)
	}
	return out
}

func (b *builder) buildField(af *parser.Field, parent, owner *Message, scope string, base Features, isExt bool) *Field {
	f := b.f
	fd := &Field{
		Name:     af.Name,
		FullName: join(scope, af.Name),
		Number:   af.Number,
		Parent:   parent,
		File:     f,
		Comments: af.Comments,
		AST:      af,
	}
	b.checkOptionNames(af.Options, fieldOptions, "field")

	// Labels.
	switch f.Edition {
	case EditionProto2:
		if af.Label == parser.LabelNone && af.Oneof == nil && !af.IsMap {
			b.errorf(af.Pos, "expected \"required\", \"optional\", or \"repeated\" label for field %q", af.Name)
		}
	case EditionProto3:
		if af.Label == parser.LabelRequired {
			b.errorf(af.Pos, "required fields are not allowed in proto3")
		}
	default:
		if af.Label == parser.LabelOptional || af.Label == parser.LabelRequired {
			b.errorf(af.Pos, "label %q is not allowed under editions; use the feature field_presence instead", af.Label)
		}
	}
	if af.IsMap && af.Label != parser.LabelNone {
		b.errorf(af.Pos, "field labels are not allowed on map fields")
	}
	if isExt && af.Label == parser.LabelRequired {
		b.errorf(af.Pos, "extensions cannot be required")
	}
	fd.Repeated = af.Label == parser.LabelRepeated || af.IsMap

	// Features, including lowering of proto2/proto3 syntax.
	feat := b.applyFeatures(base, af.Options, "field")
	var packedOpt *bool
	for _, o := range af.Options {
		switch o.SimpleName() {
		case "packed":
			v := b.boolOption(o)
			packedOpt = &v
			if f.Edition >= Edition2023 {
				b.errorf(o.Pos, "field option packed is not allowed under editions; use the feature repeated_field_encoding instead")
			}
		case "deprecated":
			fd.Deprecated = b.boolOption(o)
		case "json_name":
			if isExt {
				b.errorf(o.Pos, "option json_name is not allowed on extension fields")
			} else if o.Value.Kind != parser.ValueString {
				b.errorf(o.Pos, "json_name must be a string")
			} else {
				fd.JSONName = o.Value.Str
				fd.CustomJSONName = true
			}
		}
	}
	if !fd.CustomJSONName {
		fd.JSONName = JSONName(af.Name)
	}
	switch f.Edition {
	case EditionProto2:
		if af.Label == parser.LabelRequired {
			feat.FieldPresence = PresenceLegacyRequired
		}
		if af.Group != nil {
			feat.MessageEncoding = MessageDelimited
		}
	case EditionProto3:
		if af.Label == parser.LabelOptional {
			feat.FieldPresence = PresenceExplicit
			fd.Proto3Optional = true
		}
	}
	if packedOpt != nil {
		if *packedOpt {
			feat.RepeatedFieldEncoding = RepeatedPacked
		} else {
			feat.RepeatedFieldEncoding = RepeatedExpanded
		}
	}
	fd.Features = feat

	// Type.
	switch {
	case af.IsMap:
		b.buildMapField(fd, af, owner, scope, feat)
	case af.Group != nil:
		s, ok := b.lookup(join(scope, af.Type))
		if !ok || s.kind != symMessage {
			b.errorf(af.Pos, "internal error: group type %q not declared", af.Type)
			return nil
		}
		fd.Kind = KindMessage
		fd.MessageType = s.msg
	default:
		if k, ok := scalarKinds[af.Type]; ok {
			fd.Kind = k
			break
		}
		s, ok := b.resolveType(af.Type, scope, af.TypePos)
		if !ok {
			return nil
		}
		if s.kind == symMessage {
			fd.Kind = KindMessage
			fd.MessageType = s.msg
		} else {
			fd.Kind = KindEnum
			fd.EnumType = s.enum
		}
	}

	// Derived properties.
	if hasFieldFeature(af.Options, "message_encoding") && (fd.Kind != KindMessage || af.IsMap) {
		b.errorf(af.Pos, "only message fields can specify message encoding")
	}
	// Map fields (and their values) are always length-prefixed, even when
	// DELIMITED is inherited.
	if fd.Kind == KindMessage && feat.MessageEncoding == MessageDelimited && !af.IsMap {
		fd.Delimited = true
	}
	switch {
	case fd.Repeated:
		fd.HasPresence = false
	case af.Oneof != nil, fd.Kind == KindMessage, isExt:
		fd.HasPresence = true
	default:
		fd.HasPresence = feat.FieldPresence != PresenceImplicit
	}
	if !fd.Repeated && af.Oneof == nil && !isExt && feat.FieldPresence == PresenceLegacyRequired {
		fd.Required = true
	}
	if f.Edition >= Edition2023 && hasFieldFeature(af.Options, "field_presence") {
		switch {
		case fd.Repeated:
			b.errorf(af.Pos, "repeated fields cannot specify field presence")
		case af.Oneof != nil:
			b.errorf(af.Pos, "oneof fields cannot specify field presence")
		case isExt:
			b.errorf(af.Pos, "extensions cannot specify field presence")
		case fd.Kind == KindMessage && feat.FieldPresence == PresenceImplicit:
			b.errorf(af.Pos, "message fields cannot specify implicit presence")
		}
	}
	if fd.Repeated && fd.Kind.Packable() && feat.RepeatedFieldEncoding == RepeatedPacked {
		fd.Packed = true
	}
	if packedOpt != nil && *packedOpt && (!fd.Repeated || !fd.Kind.Packable()) {
		b.errorf(af.Pos, "[packed = true] can only be specified for repeated primitive fields")
	}
	if fd.Kind == KindString && feat.UTF8Validation == UTF8Verify {
		fd.ValidateUTF8 = true
	}
	if fd.Kind == KindEnum && !fd.HasPresence && !fd.Repeated && fd.EnumType.Closed {
		b.errorf(af.TypePos, "enum %q is closed and cannot be used with implicit presence (or in proto3 files)", fd.EnumType.FullName)
	}

	// Default value.
	for _, o := range af.Options {
		if o.SimpleName() != "default" {
			continue
		}
		switch {
		case f.Edition == EditionProto3:
			b.errorf(o.Pos, "explicit default values are not allowed in proto3")
		case fd.Repeated:
			b.errorf(o.Pos, "repeated fields cannot have default values")
		case fd.Kind == KindMessage:
			b.errorf(o.Pos, "message fields cannot have default values")
		case !fd.HasPresence:
			b.errorf(o.Pos, "implicit presence fields cannot specify defaults")
		default:
			fd.Default = b.parseDefault(fd, o.Value)
		}
	}
	return fd
}

func hasFieldFeature(opts []*parser.Option, name string) bool {
	for _, o := range opts {
		if len(o.Name) == 2 && o.Name[0] == (parser.OptionNamePart{Name: "features"}) && o.Name[1] == (parser.OptionNamePart{Name: name}) {
			return true
		}
		if len(o.Name) == 1 && o.Name[0] == (parser.OptionNamePart{Name: "features"}) && o.Value.Kind == parser.ValueMessage {
			for _, mf := range o.Value.Fields {
				if mf.Name == name && !mf.Ext {
					return true
				}
			}
		}
	}
	return false
}

func (b *builder) buildMapField(fd *Field, af *parser.Field, owner *Message, scope string, feat Features) {
	s, ok := b.lookup(join(scope, mapEntryName(af.Name)))
	if !ok || s.kind != symMessage || !s.msg.IsMapEntry {
		b.errorf(af.Pos, "internal error: map entry for %q not declared", af.Name)
		return
	}
	entry := s.msg
	entry.Features = feat
	fd.Kind = KindMessage
	fd.MessageType = entry
	fd.IsMap = true

	key := &Field{Name: "key", FullName: join(entry.FullName, "key"), Number: 1, Parent: entry, File: b.f, Features: feat, HasPresence: true}
	switch kk, ok := scalarKinds[af.MapKey]; {
	case !ok:
		b.errorf(af.Pos, "key in map fields cannot be of type %q; must be an integral or string type", af.MapKey)
		return
	case kk == KindFloat || kk == KindDouble || kk == KindBytes:
		b.errorf(af.Pos, "key in map fields cannot be float/double, bytes or message types")
		return
	default:
		key.Kind = kk
	}
	val := &Field{Name: "value", FullName: join(entry.FullName, "value"), Number: 2, Parent: entry, File: b.f, Features: feat, HasPresence: true}
	if k, ok := scalarKinds[af.MapValue]; ok {
		val.Kind = k
	} else {
		s, ok := b.resolveType(af.MapValue, scope, af.TypePos)
		if !ok {
			return
		}
		if s.kind == symMessage {
			val.Kind = KindMessage
			val.MessageType = s.msg
		} else {
			val.Kind = KindEnum
			val.EnumType = s.enum
		}
	}
	if val.Kind == KindEnum && (len(val.EnumType.Values) == 0 || val.EnumType.Values[0].Number != 0) {
		b.errorf(af.TypePos, "enum value in map must define 0 as the first value")
	}
	for _, kv := range []*Field{key, val} {
		if kv.Kind == KindString && feat.UTF8Validation == UTF8Verify {
			kv.ValidateUTF8 = true
		}
	}
	entry.Fields = []*Field{key, val}
	fd.MapKey = key
	fd.MapValue = val
}

func (b *builder) parseDefault(fd *Field, v *parser.Value) *DefaultValue {
	dv := &DefaultValue{}
	bad := func() *DefaultValue {
		b.errorf(v.Pos, "invalid default value for field %q of type %s", fd.Name, fd.Kind)
		return nil
	}
	signed := func(min, max int64) *DefaultValue {
		if v.Kind != parser.ValueInt {
			return bad()
		}
		if v.Neg {
			if v.Int > uint64(-(min+1))+1 {
				return bad()
			}
			dv.Int = -int64(v.Int-1) - 1
		} else {
			if v.Int > uint64(max) {
				return bad()
			}
			dv.Int = int64(v.Int)
		}
		return dv
	}
	unsigned := func(max uint64) *DefaultValue {
		if v.Kind != parser.ValueInt || (v.Neg && v.Int != 0) || v.Int > max {
			return bad()
		}
		dv.Uint = v.Int
		return dv
	}
	switch fd.Kind {
	case KindInt32, KindSint32, KindSfixed32:
		return signed(math.MinInt32, math.MaxInt32)
	case KindInt64, KindSint64, KindSfixed64:
		return signed(math.MinInt64, math.MaxInt64)
	case KindUint32, KindFixed32:
		return unsigned(math.MaxUint32)
	case KindUint64, KindFixed64:
		return unsigned(math.MaxUint64)
	case KindFloat, KindDouble:
		switch v.Kind {
		case parser.ValueInt:
			dv.Float = float64(v.Int)
			if v.Neg {
				dv.Float = -dv.Float
			}
		case parser.ValueFloat:
			dv.Float = v.Float
		case parser.ValueIdent:
			switch strings.ToLower(v.Ident) {
			case "inf", "infinity":
				dv.Float = math.Inf(1)
				if v.Neg {
					dv.Float = math.Inf(-1)
				}
			case "nan":
				dv.Float = math.NaN()
			default:
				return bad()
			}
		default:
			return bad()
		}
		if fd.Kind == KindFloat {
			dv.Float = float64(float32(dv.Float))
		}
		return dv
	case KindBool:
		if v.Kind != parser.ValueIdent || v.Neg || (v.Ident != "true" && v.Ident != "false") {
			return bad()
		}
		dv.Bool = v.Ident == "true"
		return dv
	case KindString, KindBytes:
		if v.Kind != parser.ValueString {
			return bad()
		}
		dv.String = v.Str
		return dv
	case KindEnum:
		if v.Kind != parser.ValueIdent || v.Neg {
			return bad()
		}
		for _, ev := range fd.EnumType.Values {
			if ev.Name == v.Ident {
				dv.Enum = ev
				return dv
			}
		}
		b.errorf(v.Pos, "enum type %q has no value named %q", fd.EnumType.FullName, v.Ident)
		return nil
	}
	return bad()
}

func (b *builder) validateMessage(m *Message) {
	if m.IsMapEntry {
		return
	}
	am := m.AST
	byNum := map[int32]*Field{}
	byName := map[string]*Field{}
	for _, fd := range m.Fields {
		pos := fd.AST.Pos
		if fd.Number < 1 || fd.Number > MaxFieldNumber {
			b.errorf(pos, "field number %d for %q is out of range (1 to %d)", fd.Number, fd.Name, MaxFieldNumber)
		} else if fd.Number >= 19000 && fd.Number <= 19999 {
			b.errorf(pos, "field numbers 19000 through 19999 are reserved for the protocol buffer library implementation (field %q)", fd.Name)
		}
		if prev, ok := byNum[fd.Number]; ok {
			b.errorf(pos, "field number %d has already been used in %q by field %q", fd.Number, m.FullName, prev.Name)
		}
		byNum[fd.Number] = fd
		byName[fd.Name] = fd
		for _, r := range am.ReservedRanges {
			if int64(fd.Number) >= r.Start && int64(fd.Number) <= r.End {
				b.errorf(pos, "field %q uses reserved number %d", fd.Name, fd.Number)
			}
		}
		if slices.Contains(am.ReservedNames, fd.Name) {
			b.errorf(pos, "field name %q is reserved", fd.Name)
		}
		for _, r := range am.ExtensionRanges {
			if int64(fd.Number) >= r.Start && int64(fd.Number) <= r.End {
				b.errorf(pos, "extension range %d to %d includes field %q (%d)", r.Start, r.End, fd.Name, fd.Number)
			}
		}
	}
	b.checkJSONNames(m)
	checkRanges := func(rs []*parser.Range, what string) {
		for _, r := range rs {
			if r.Start < 1 || r.End > MaxFieldNumber || r.Start > r.End {
				b.errorf(r.Pos, "invalid %s range %d to %d", what, r.Start, r.End)
			}
		}
	}
	checkRanges(am.ReservedRanges, "reserved")
	for _, r := range am.ExtensionRanges {
		if r.Start < 1 || r.Start > r.End || (r.End > MaxFieldNumber && r.End != math.MaxInt32) {
			b.errorf(r.Pos, "invalid extension range %d to %d", r.Start, r.End)
		}
	}
	all := append(slices.Clone(am.ReservedRanges), am.ExtensionRanges...)
	for i, r := range all {
		for _, r2 := range all[i+1:] {
			if r.Start <= r2.End && r2.Start <= r.End {
				b.errorf(r2.Pos, "ranges %d to %d and %d to %d overlap", r.Start, r.End, r2.Start, r2.End)
			}
		}
	}
}

func (b *builder) validateEnum(e *Enum) {
	ae := e.AST
	if len(e.Values) == 0 {
		b.errorf(ae.Pos, "enum %q must contain at least one value", e.Name)
		return
	}
	if !e.Closed && e.Values[0].Number != 0 {
		b.errorf(ae.Values[0].Pos, "the first enum value of open enum %q must be zero", e.FullName)
	}
	allowAlias := false
	for _, o := range ae.Options {
		if o.SimpleName() == "allow_alias" {
			allowAlias = b.boolOption(o)
		}
	}
	byNum := map[int32]string{}
	aliased := false
	for i, v := range e.Values {
		pos := ae.Values[i].Pos
		if prev, ok := byNum[v.Number]; ok {
			aliased = true
			if !allowAlias {
				b.errorf(pos, "%q uses the same enum value as %q; if this is intended, set 'option allow_alias = true;' on the enum", v.Name, prev)
			}
		} else {
			byNum[v.Number] = v.Name
		}
		for _, r := range ae.ReservedRanges {
			if int64(v.Number) >= r.Start && int64(v.Number) <= r.End {
				b.errorf(pos, "enum value %q uses reserved number %d", v.Name, v.Number)
			}
		}
		if slices.Contains(ae.ReservedNames, v.Name) {
			b.errorf(pos, "enum value name %q is reserved", v.Name)
		}
	}
	if allowAlias && !aliased {
		b.errorf(ae.Pos, "enum %q declares 'option allow_alias = true;' but has no aliases", e.FullName)
	}
}

// JSONName returns the default ProtoJSON name of a field, matching protoc:
// underscores are removed and the following character is upper-cased.
func JSONName(name string) string {
	var sb strings.Builder
	upper := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_':
			upper = true
		case upper:
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			sb.WriteByte(c)
			upper = false
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// checkJSONNames reports JSON name conflicts the way protoc does: always
// when a custom json_name is involved, and for default names only when the
// message's json_format feature is ALLOW.
func (b *builder) checkJSONNames(m *Message) {
	seen := map[string]*Field{}
	for _, fd := range m.Fields {
		prev, ok := seen[fd.JSONName]
		if !ok {
			seen[fd.JSONName] = fd
			continue
		}
		if !fd.CustomJSONName && !prev.CustomJSONName && m.Features.JSONFormat != JSONAllow {
			continue
		}
		kind := func(f *Field) string {
			if f.CustomJSONName {
				return "custom"
			}
			return "default"
		}
		b.errorf(fd.AST.Pos, "the %s JSON name of field %q (%q) conflicts with the %s JSON name of field %q",
			kind(fd), fd.Name, fd.JSONName, kind(prev), prev.Name)
	}
}
