// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package parser

// File is the syntax tree of a single .proto file.
type File struct {
	Name string // path the file was loaded as, e.g. "foo/bar.proto"

	// Syntax is "proto2", "proto3" or "editions".
	Syntax string
	// Edition is set when Syntax == "editions", e.g. "2023".
	Edition    string
	SyntaxPos  Pos
	Package    string
	PackagePos Pos
	Imports    []*Import
	Options    []*Option
	Messages   []*Message
	Enums      []*Enum
	Extends    []*Extend
	Services   []*Service
}

// Import is an import statement.
type Import struct {
	Pos    Pos
	Path   string
	Public bool
	Weak   bool
	// Option marks an edition 2024 "import option" statement.
	Option bool
}

// OptionNamePart is one dotted component of an option name. Ext is set for
// parenthesised extension names such as (foo.bar).
type OptionNamePart struct {
	Name string
	Ext  bool
}

// Option is an option assignment: `option a.(b).c = value;` or a field
// option inside [...].
type Option struct {
	Pos   Pos
	Name  []OptionNamePart
	Value *Value
}

// SimpleName returns the option name if it is a single non-extension
// component, otherwise "".
func (o *Option) SimpleName() string {
	if len(o.Name) == 1 && !o.Name[0].Ext {
		return o.Name[0].Name
	}
	return ""
}

// ValueKind is the kind of a constant value.
type ValueKind int

const (
	ValueIdent ValueKind = iota
	ValueInt
	ValueFloat
	ValueString
	ValueMessage
	ValueList
)

// Value is a constant: an identifier, number, string or text-format
// aggregate.
type Value struct {
	Pos  Pos
	Kind ValueKind

	// Neg is set when the value was preceded by '-'. It applies to
	// ValueInt, ValueFloat and ValueIdent (e.g. -inf).
	Neg bool

	Ident  string  // ValueIdent
	Int    uint64  // ValueInt: magnitude
	Float  float64 // ValueFloat (sign already applied)
	Str    string  // ValueString
	Fields []*MessageField
	List   []*Value

	// Text is the source text for numeric literals.
	Text string
}

// MessageField is a field inside a text-format aggregate value.
type MessageField struct {
	Pos   Pos
	Name  string
	Ext   bool // [ext.name] or [type.url/Name]
	Value *Value
}

// Label is a field label.
type Label int

const (
	LabelNone Label = iota
	LabelOptional
	LabelRequired
	LabelRepeated
)

func (l Label) String() string {
	switch l {
	case LabelOptional:
		return "optional"
	case LabelRequired:
		return "required"
	case LabelRepeated:
		return "repeated"
	}
	return ""
}

// Field is a message field, group field, map field or extension field.
type Field struct {
	Pos      Pos
	Comments string
	Label    Label
	Name     string
	// Type is the scalar type keyword or (possibly qualified) message/enum
	// type name. For groups it is the group's message name. For maps it is
	// empty.
	Type    string
	TypePos Pos
	Number  int32
	Options []*Option

	// For map fields.
	MapKey   string
	MapValue string
	IsMap    bool

	// Group is the message body of a group field.
	Group *Message

	// Oneof is the enclosing oneof, if any.
	Oneof *Oneof
}

// Range is an inclusive range of field or enum numbers.
type Range struct {
	Pos   Pos
	Start int64
	End   int64 // inclusive; for "max" this is set by the caller-supplied max.
	Max   bool
}

// Oneof is a oneof declaration.
type Oneof struct {
	Pos      Pos
	Comments string
	Name     string
	Fields   []*Field
	Options  []*Option
}

// Message is a message declaration (or the body of a group).
type Message struct {
	Pos      Pos
	Comments string
	Name     string
	// Fields lists all fields in declaration order, including oneof
	// members (which also appear in their Oneof).
	Fields          []*Field
	Oneofs          []*Oneof
	Messages        []*Message
	Enums           []*Enum
	Extends         []*Extend
	ExtensionRanges []*Range
	ReservedRanges  []*Range
	ReservedNames   []string
	Options         []*Option
}

// EnumValue is a single enum constant.
type EnumValue struct {
	Pos      Pos
	Comments string
	Name     string
	Number   int32
	Options  []*Option
}

// Enum is an enum declaration.
type Enum struct {
	Pos            Pos
	Comments       string
	Name           string
	Values         []*EnumValue
	Options        []*Option
	ReservedRanges []*Range
	ReservedNames  []string
}

// Extend is an extend block.
type Extend struct {
	Pos      Pos
	Extendee string
	Fields   []*Field
}

// Service is a service declaration. Services are parsed but not used for
// code generation.
type Service struct {
	Pos  Pos
	Name string
}
