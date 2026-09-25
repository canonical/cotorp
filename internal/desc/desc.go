// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package desc builds linked, validated descriptors from parsed .proto
// files. Every syntax (proto2, proto3 and editions) is lowered to a common
// model of resolved features, so code generators only need to consult the
// per-field properties (presence, packing, delimited encoding, UTF-8
// validation, enum openness).
package desc

import (
	"github.com/canonical/cotorp/internal/parser"
)

// Edition identifies the syntax or edition of a file. The numeric values
// match google.protobuf.Edition.
type Edition int

const (
	EditionProto2 Edition = 998
	EditionProto3 Edition = 999
	Edition2023   Edition = 1000
	Edition2024   Edition = 1001
)

func (e Edition) String() string {
	switch e {
	case EditionProto2:
		return "proto2"
	case EditionProto3:
		return "proto3"
	case Edition2023:
		return "2023"
	case Edition2024:
		return "2024"
	}
	return "unknown"
}

// Feature values. The numeric values match descriptor.proto.
type (
	FieldPresence         int
	EnumType              int
	RepeatedFieldEncoding int
	UTF8Validation        int
	MessageEncoding       int
	JSONFormat            int
)

const (
	PresenceExplicit       FieldPresence = 1
	PresenceImplicit       FieldPresence = 2
	PresenceLegacyRequired FieldPresence = 3

	EnumOpen   EnumType = 1
	EnumClosed EnumType = 2

	RepeatedPacked   RepeatedFieldEncoding = 1
	RepeatedExpanded RepeatedFieldEncoding = 2

	UTF8Verify UTF8Validation = 2
	UTF8None   UTF8Validation = 3

	MessageLengthPrefixed MessageEncoding = 1
	MessageDelimited      MessageEncoding = 2

	JSONAllow            JSONFormat = 1
	JSONLegacyBestEffort JSONFormat = 2
)

// Features is the resolved feature set at some point in the descriptor
// hierarchy.
type Features struct {
	FieldPresence         FieldPresence
	EnumType              EnumType
	RepeatedFieldEncoding RepeatedFieldEncoding
	UTF8Validation        UTF8Validation
	MessageEncoding       MessageEncoding
	JSONFormat            JSONFormat
}

func defaultFeatures(e Edition) Features {
	switch e {
	case EditionProto2:
		return Features{PresenceExplicit, EnumClosed, RepeatedExpanded, UTF8None, MessageLengthPrefixed, JSONLegacyBestEffort}
	case EditionProto3:
		return Features{PresenceImplicit, EnumOpen, RepeatedPacked, UTF8Verify, MessageLengthPrefixed, JSONAllow}
	default:
		return Features{PresenceExplicit, EnumOpen, RepeatedPacked, UTF8Verify, MessageLengthPrefixed, JSONAllow}
	}
}

// Kind is a field's type. Values match FieldDescriptorProto.Type.
type Kind int

const (
	KindDouble   Kind = 1
	KindFloat    Kind = 2
	KindInt64    Kind = 3
	KindUint64   Kind = 4
	KindInt32    Kind = 5
	KindFixed64  Kind = 6
	KindFixed32  Kind = 7
	KindBool     Kind = 8
	KindString   Kind = 9
	KindGroup    Kind = 10 // unused: groups are KindMessage with Delimited set
	KindMessage  Kind = 11
	KindBytes    Kind = 12
	KindUint32   Kind = 13
	KindEnum     Kind = 14
	KindSfixed32 Kind = 15
	KindSfixed64 Kind = 16
	KindSint32   Kind = 17
	KindSint64   Kind = 18
)

var scalarKinds = map[string]Kind{
	"double":   KindDouble,
	"float":    KindFloat,
	"int64":    KindInt64,
	"uint64":   KindUint64,
	"int32":    KindInt32,
	"fixed64":  KindFixed64,
	"fixed32":  KindFixed32,
	"bool":     KindBool,
	"string":   KindString,
	"bytes":    KindBytes,
	"uint32":   KindUint32,
	"sfixed32": KindSfixed32,
	"sfixed64": KindSfixed64,
	"sint32":   KindSint32,
	"sint64":   KindSint64,
}

func (k Kind) String() string {
	for n, v := range scalarKinds {
		if v == k {
			return n
		}
	}
	switch k {
	case KindMessage:
		return "message"
	case KindGroup:
		return "group"
	case KindEnum:
		return "enum"
	}
	return "invalid"
}

// Packable reports whether repeated fields of this kind may use packed
// encoding.
func (k Kind) Packable() bool {
	switch k {
	case KindString, KindBytes, KindMessage, KindGroup:
		return false
	}
	return true
}

// File is a linked .proto file.
type File struct {
	Path    string
	AST     *parser.File
	Package string
	Edition Edition

	Imports       []*File
	PublicImports []*File

	Messages   []*Message
	Enums      []*Enum
	Extensions []*Field

	// AllMessages and AllEnums list every message and enum declared in the
	// file, including nested ones and synthesized map entries, in
	// declaration order (parents before children).
	AllMessages []*Message
	AllEnums    []*Enum

	// GoPackage is the value of the go_package option, if any.
	GoPackage string

	Features Features

	symbols map[string]symbol
}

// Message is a linked message (or group) type.
type Message struct {
	Name     string
	FullName string
	File     *File
	Parent   *Message // nil for top-level messages
	Comments string

	// Fields in declaration order, including oneof members.
	Fields []*Field
	// Oneofs lists real (non-synthetic) oneofs.
	Oneofs     []*Oneof
	Messages   []*Message
	Enums      []*Enum
	Extensions []*Field

	IsMapEntry      bool
	Deprecated      bool
	ExtensionRanges [][2]int32 // inclusive

	Features Features
	AST      *parser.Message
}

// Oneof is a real oneof.
type Oneof struct {
	Name     string
	FullName string
	Message  *Message
	Fields   []*Field
	Comments string
}

// Field is a linked field or extension.
type Field struct {
	Name     string
	FullName string
	Number   int32
	Kind     Kind
	// Parent is the containing message (for extensions, the message where
	// the extension was declared, or nil at file scope).
	Parent *Message
	File   *File

	Repeated bool
	Required bool
	// HasPresence reports whether the field tracks presence (pointer or
	// nillable representation).
	HasPresence bool
	// Proto3Optional reports a proto3 field with the optional keyword.
	Proto3Optional bool
	// Packed reports that repeated values are written in packed form.
	Packed bool
	// Delimited reports group (start/end tag) encoding for message fields.
	Delimited bool
	// ValidateUTF8 reports that string contents must be valid UTF-8.
	ValidateUTF8 bool

	MessageType *Message
	EnumType    *Enum
	Oneof       *Oneof

	// Map fields.
	IsMap    bool
	MapKey   *Field
	MapValue *Field

	// Extendee is set for extension fields.
	Extendee *Message

	Default *DefaultValue

	// JSONName is the field's ProtoJSON name: the json_name option if set,
	// otherwise the lowerCamelCase form of Name.
	JSONName string
	// CustomJSONName reports that JSONName came from the json_name option.
	CustomJSONName bool

	Deprecated bool
	Comments   string
	Features   Features
	AST        *parser.Field
}

// DefaultValue is an explicit field default.
type DefaultValue struct {
	Int    int64
	Uint   uint64
	Float  float64
	Bool   bool
	String string // also bytes
	Enum   *EnumValue
}

// Enum is a linked enum type.
type Enum struct {
	Name     string
	FullName string
	File     *File
	Parent   *Message
	Values   []*EnumValue
	Closed   bool
	Comments string

	Deprecated bool
	Features   Features
	AST        *parser.Enum
}

// EnumValue is a linked enum constant.
type EnumValue struct {
	Name       string
	FullName   string
	Number     int32
	Enum       *Enum
	Comments   string
	Deprecated bool
}

// HasRequired reports whether a message transitively contains required
// fields. It is computed lazily over the message graph.
func (m *Message) HasRequired() bool {
	return hasRequired(m, map[*Message]bool{})
}

func hasRequired(m *Message, seen map[*Message]bool) bool {
	if seen[m] {
		return false
	}
	seen[m] = true
	for _, f := range m.Fields {
		if f.Required {
			return true
		}
		if f.IsMap {
			if f.MapValue.MessageType != nil && hasRequired(f.MapValue.MessageType, seen) {
				return true
			}
			continue
		}
		if f.MessageType != nil && hasRequired(f.MessageType, seen) {
			return true
		}
	}
	return false
}
