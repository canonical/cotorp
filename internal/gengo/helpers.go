// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import "github.com/canonical/cotorp/internal/desc"

// Code that would otherwise repeat in every message is emitted once per file
// as an unexported function. As with constants, several files may share a Go
// package, so each function name starts with the file's prefix, and a file
// declares only the functions it uses. With Options.Helpers, every function
// is instead exported once from a shared package, which the files import.
//
// Helpers never take a message as an argument: calling a message's methods
// through an interface or type parameter would make messages escape to the
// heap. Methods call their own methods and pass the results to helpers, or
// pass a method expression such as (*Msg).ProtoSize together with a field
// that is already a pointer (see genSizeMsg): after inlining, the compiler
// devirtualizes the call and nothing new escapes.

// helperDef is a function that generated code may call. doc is its doc
// comment, emitted in the shared helper package where it is exported.
type helperDef struct {
	suffix string // name without the file prefix
	gen    func(fg *fileGen, name string)
	doc    string
}

// helperDefs lists every helper, in declaration order.
var helperDefs = []helperDef{
	// Binary sizing and writing.
	{"SizeLen", genSizeLen,
		"SizeLen returns the encoded size of a length-delimited value of l bytes: the length prefix plus l."},
	{"SizeVarint", genSizeVarint,
		"SizeVarint returns the size of an implicit-presence varint field u with a tag of tag bytes, or 0 if u is zero."},
	{"SizeBool", genSizeBool,
		"SizeBool returns the size of an implicit-presence bool field, or 0 if v is false."},
	{"SizeFixed", genSizeFixed,
		"SizeFixed returns tag+size, the size of a fixed-width field, if set, or 0."},
	{"SizeLenField", genSizeLenField,
		"SizeLenField returns the size of an implicit-presence string, bytes or packed field of l bytes, or 0 if l is zero."},
	{"SizePresentBytes", genSizePresentBytes,
		"SizePresentBytes returns the size of an explicit-presence bytes field, or 0 if v is nil."},
	{"SizeMsg", genSizeMsg,
		"SizeMsg returns the size of message field p, measured by size, or 0 if p is nil."},
	{"SizeMsgs", genSizeMsgs,
		"SizeMsgs returns the size of repeated message field s, whose elements are measured by size."},
	{"SizeOpt", genSizeOpt,
		"SizeOpt returns the size of explicit-presence field p, measured by size, or 0 if p is nil."},
	{"SizeOptFixed", genSizeOptFixed,
		"SizeOptFixed returns tag+size for explicit-presence fixed-width field p, or 0 if p is nil."},
	{"SizePacked", genSizePacked,
		"SizePacked returns the size of packed field s, whose elements are measured by size, or 0 if s is empty."},
	{"SizeEach", genSizeEach,
		"SizeEach returns the size of repeated field s, whose elements are measured by size and each carry a tag."},
	{"VarintLen", genVarintLen,
		"VarintLen returns the number of bytes in the varint encoding of v."},
	{"Sint32Len", genSint32Len,
		"Sint32Len returns the number of bytes in the zigzag varint encoding of v."},
	{"Sint64Len", genSint64Len,
		"Sint64Len returns the number of bytes in the zigzag varint encoding of v."},
	{"BoolLen", genBoolLen,
		"BoolLen returns the size of an encoded bool, which is 1."},
	{"BytesLen", genBytesLen,
		"BytesLen returns the size of length-delimited value v with its length prefix."},
	{"CheckUTF8", genCheckUTF8,
		"CheckUTF8 returns an error with message msg if s is not valid UTF-8, and err otherwise."},
	{"CopyUnknown", genCopyUnknown,
		"CopyUnknown copies the unknown fields u to the end of b[:i] and returns the new i."},
	{"AppendBinary", genAppendBinary,
		"AppendBinary grows b by size, marshals into the new space with marshal and returns the result, or an error if marshal fails or writes another count."},
	{"PutVarint", genPutVarint,
		"PutVarint writes varint u ending at b[i] and returns the new i."},
	{"PutFixed32", genPutFixed32,
		"PutFixed32 writes u as four little-endian bytes ending at b[i] and returns the new i."},
	{"PutFixed64", genPutFixed64,
		"PutFixed64 writes u as eight little-endian bytes ending at b[i] and returns the new i."},
	{"PutBool", genPutBool,
		"PutBool writes v as one byte at b[i-1] and returns the new i."},
	{"PutVarintField", genPutVarintField,
		"PutVarintField writes varint field u with its one-byte tag ending at b[i] and returns the new i, or i if u is zero."},
	{"PutStringField", genPutStringField,
		"PutStringField writes string field v with its length and one-byte tag ending at b[i] and returns the new i, or i if v is empty."},
	{"PutBytesField", genPutBytesField,
		"PutBytesField writes bytes field v with its length and one-byte tag ending at b[i] and returns the new i, or i if v is empty."},
	{"PutFixed32Field", genPutFixed32Field,
		"PutFixed32Field writes fixed32 field u with its one-byte tag ending at b[i] and returns the new i, or i if u is zero."},
	{"PutFixed64Field", genPutFixed64Field,
		"PutFixed64Field writes fixed64 field u with its one-byte tag ending at b[i] and returns the new i, or i if u is zero."},
	{"PutBoolField", genPutBoolField,
		"PutBoolField writes bool field v with its one-byte tag ending at b[i] and returns the new i, or i if v is false."},
	{"PutVarintField2", genPutVarintField2,
		"PutVarintField2 is PutVarintField with a two-byte tag."},
	{"PutFixed32Field2", genPutFixed32Field2,
		"PutFixed32Field2 is PutFixed32Field with a two-byte tag."},
	{"PutFixed64Field2", genPutFixed64Field2,
		"PutFixed64Field2 is PutFixed64Field with a two-byte tag."},
	{"PutBoolField2", genPutBoolField2,
		"PutBoolField2 is PutBoolField with a two-byte tag."},
	{"SortedKeys", genSortedKeys,
		"SortedKeys appends the keys of m to keys and sorts them."},
	{"If", genIf,
		"If returns a if c, and b otherwise. Both are evaluated."},
	// Binary reading.
	{"Depth", genDepth,
		"Depth returns an error if depth reaches the nesting limit."},
	{"ReadBytes", genReadBytes,
		"ReadBytes returns the length-delimited value at the start of b and the number of bytes it occupies, or -1 if it is malformed."},
	{"SkipField", genSkipField,
		"SkipField returns the length of the value of the field with tag t at b, or an error if it is malformed."},
	{"SkipGroup", genSkipGroup,
		"SkipGroup returns the length of the group with field number num at b, up to and including its end tag."},
	{"Unknown", genUnknown,
		"Unknown skips the field with tag t at b and appends it, with a canonical tag, to *unk."},
	{"End", genEnd,
		"End returns err, or a parse error if b is not empty."},
	{"Varint", genVarint,
		"Varint decodes the varint at b into *p and returns the number of bytes consumed, or 0 or less if it is malformed."},
	{"Sint32", genSint32,
		"Sint32 decodes the zigzag varint at b into *p and returns the number of bytes consumed, or 0 or less if it is malformed."},
	{"Sint64", genSint64,
		"Sint64 decodes the zigzag varint at b into *p and returns the number of bytes consumed, or 0 or less if it is malformed."},
	{"Bool", genBool,
		"Bool decodes the varint at b as a bool into *p and returns the number of bytes consumed, or 0 or less if it is malformed."},
	{"Fixed32", genFixed32,
		"Fixed32 decodes the four little-endian bytes at b into *p and returns 4, or -1 if b is shorter."},
	{"Fixed64", genFixed64,
		"Fixed64 decodes the eight little-endian bytes at b into *p and returns 8, or -1 if b is shorter."},
	{"Float", genFloat,
		"Float decodes the four little-endian bytes at b as a float32 into *p and returns 4, or -1 if b is shorter."},
	{"Double", genDouble,
		"Double decodes the eight little-endian bytes at b as a float64 into *p and returns 8, or -1 if b is shorter."},
	{"Bytes", genBytes,
		"Bytes decodes the length-delimited value at b into a copy at *p and returns the number of bytes consumed, or -1 if it is malformed."},
	{"String", genString,
		"String decodes the length-delimited value at b into *p and returns the number of bytes consumed, or -1 if it is malformed; with errUTF8 set, invalid UTF-8 is an error with that message."},
	{"PackedVarint", genPackedVarint,
		"PackedVarint appends the elements of the packed varint field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedSint32", genPackedSint32,
		"PackedSint32 appends the elements of the packed sint32 field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedSint64", genPackedSint64,
		"PackedSint64 appends the elements of the packed sint64 field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedBool", genPackedBool,
		"PackedBool appends the elements of the packed bool field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedFixed32", genPackedFixed32,
		"PackedFixed32 appends the elements of the packed fixed32 field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedFixed64", genPackedFixed64,
		"PackedFixed64 appends the elements of the packed fixed64 field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedFloat", genPackedFloat,
		"PackedFloat appends the elements of the packed float field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedDouble", genPackedDouble,
		"PackedDouble appends the elements of the packed double field at b to *s and returns the number of bytes consumed, or -1 if it is malformed."},
	{"ClosedEnum", genClosedEnum,
		"ClosedEnum decodes the varint at b as a closed enum value and calls set with it if it is declared, or keeps it in *unk as an unknown field with tag; it returns the number of bytes consumed, or -1 if it is malformed."},
	{"PackedClosedEnum", genPackedClosedEnum,
		"PackedClosedEnum is ClosedEnum for the elements of a packed field."},
	{"Group", genGroup,
		"Group returns the contents of the group with field number num at b, without its end tag, and the length of the whole group."},
	{"New", genNew,
		"New allocates *p and returns it."},
	{"Grow", genGrow,
		"Grow appends a zero element to *s and returns a pointer to it."},
	{"Alloc", genAlloc,
		"Alloc allocates *p if it is nil and returns it."},
	{"MapSet", genMapSet,
		"MapSet stores v under k in *m, allocating the map if it is nil."},
	// Enums and getters.
	{"Invert", genInvert,
		"Invert returns the map from m's values to its keys."},
	{"EnumString", genEnumString,
		"EnumString returns the name of enum value v in names, or its number."},
	{"Deref", genDeref,
		"Deref returns *p, or def if p is nil."},
	{"BytesOr", genBytesOr,
		"BytesOr returns v, or a copy of def if v is nil."},
	// JSON marshal.
	{"MarshalTo", genMarshalTo,
		"MarshalTo writes the JSON value appended by f to e."},
	{"AppendEnum", genAppendEnum,
		"AppendEnum appends the name of enum value v in names as a JSON string, or its number."},
	{"AppendFloat", genAppendFloat,
		"AppendFloat appends f as a JSON number, or as the string NaN, Infinity or -Infinity."},
	{"Quote", genQuote,
		"Quote appends s as a JSON string, or nothing if err is set."},
	{"AppendMsg", genAppendMsg,
		"AppendMsg appends lit and then the JSON encoding of message p by f, or nothing if p is nil or err is set."},
	{"AppendList", genAppendList,
		"AppendList appends lit, which ends with the opening bracket, and the JSON array of the elements of s encoded by f, or nothing if s is empty or err is set."},
	{"AppendMap", genAppendMap,
		"AppendMap appends lit, which ends with the opening brace, and the JSON object of the entries of m in key order, encoded by fk and fv, or nothing if m is empty or err is set."},
	{"CloseObject", genCloseObject,
		"CloseObject closes the JSON object started at b[start], whose members each begin with a comma, or appends {} if it has none."},
	// JSON unmarshal.
	{"MergeJSON", genMergeJSON,
		"MergeJSON decodes the JSON value in b with merge and rejects trailing data."},
	{"MergeFrom", genMergeFrom,
		"MergeFrom decodes a JSON value from d with merge, or from a strict copy of the value if d allows invalid UTF-8."},
	{"StrictDecoder", genStrictDecoder,
		"StrictDecoder returns d, or a decoder over its next value that rejects invalid UTF-8 if d does not."},
	{"ExpectJSON", genExpectJSON,
		"ExpectJSON reads a token from d, which must be of the given kind."},
	{"OpenObject", genOpenObject,
		"OpenObject reads the opening brace of the JSON object of message name and returns its first key, or EndObject for an empty object or null."},
	{"NextKey", genNextKey,
		"NextKey returns the next key of a JSON object, or its closing brace, unless err is set."},
	{"Field", genField,
		"Field parses the value of field key into p, rejecting a duplicate key."},
	{"OptField", genOptField,
		"OptField parses the value of explicit-presence field key into a new *p, or leaves it on null."},
	{"RepField", genRepField,
		"RepField parses the JSON array of repeated field key into s, or leaves it on null."},
	{"MapField", genMapField,
		"MapField parses the JSON object of map field key into m, with pk parsing keys and pv values, or leaves it on null."},
	{"OneofField", genOneofField,
		"OneofField stores wrapper w in oneof field p and parses the value of field key into w's field v, rejecting a second member; null leaves p unless nullOK."},
	{"JSONArray", genJSONArray,
		"JSONArray parses a JSON array, appending each element parsed by parse to *s; null elements are rejected unless nullOK."},
	{"JSONMap", genJSONMap,
		"JSONMap parses a JSON object into *m, with pk parsing keys and pv values; null values are rejected unless nullOK."},
	{"JSONInt", genJSONInt,
		"JSONInt parses a JSON integer, as a number or a string, into *p, or leaves it on null."},
	{"JSONUint", genJSONUint,
		"JSONUint parses a JSON unsigned integer, as a number or a string, into *p, or leaves it on null."},
	{"JSONFloat", genJSONFloat,
		"JSONFloat parses a JSON number, or a string holding a number, NaN, Infinity or -Infinity, into *p, or leaves it on null."},
	{"JSONBool", genJSONBool,
		"JSONBool parses a JSON boolean into *p, or leaves it on null."},
	{"JSONString", genJSONString,
		"JSONString parses a JSON string into *p, or leaves it on null."},
	{"JSONBytes", genJSONBytes,
		"JSONBytes parses a base64 string, in the standard or URL alphabet with or without padding, into *p, or leaves it on null."},
	{"JSONHex", genJSONHex,
		"JSONHex parses a hex string into *p, or leaves it on null."},
	{"JSONEnum", genJSONEnum,
		"JSONEnum parses a value of enum, given by name in values or by number, into *p; null sets it to zero if nullOK and otherwise leaves it."},
	{"JSONMsg", genJSONMsg,
		"JSONMsg merges a JSON object into the message at *p, allocating it, or leaves it on null."},
	{"JSONMsgNull", genJSONMsgNull,
		"JSONMsgNull is JSONMsg for google.protobuf.Value, for which null is a value."},
	{"ParseInt", genParseInt,
		"ParseInt parses integer token tok of the given bit width, accepting the exponent and fraction forms of whole numbers."},
	{"ParseUint", genParseUint,
		"ParseUint parses unsigned integer token tok of the given bit width, accepting the exponent and fraction forms of whole numbers."},
	{"ExactInt", genExactInt,
		"ExactInt returns the value of JSON number s, or nil if it is not a whole number."},
	{"ParseFloat", genParseFloat,
		"ParseFloat parses number token tok of the given bit width, or the strings NaN, Infinity and -Infinity."},
	{"JSONNumber", genJSONNumber,
		"JSONNumber returns the text of number token tok, or of a string token holding a number."},
	{"IntKey", genIntKey,
		"IntKey parses map key s as an integer of K's width."},
	{"UintKey", genUintKey,
		"UintKey parses map key s as an unsigned integer of K's width."},
	{"BoolKey", genBoolKey,
		"BoolKey parses map key s as true or false."},
	{"StringKey", genStringKey,
		"StringKey returns map key s."},
	{"JSONError", genJSONError,
		"JSONError returns the error \"proto: <name>: <msg>\"."},
}

// fn returns the name of the helper with the given suffix, declaring it in
// this file, or referring to the shared helper package (see
// Options.Helpers) where it is exported under the suffix.
func (fg *fileGen) fn(suffix string) string {
	if name, ok := fg.funcs[suffix]; ok {
		return name
	}
	switch {
	case fg.shared:
		fg.funcs[suffix] = suffix
	case fg.g.helpers != nil:
		return fg.helpersAlias() + "." + suffix
	default:
		fg.funcs[suffix] = fg.constName(suffix)
	}
	return fg.funcs[suffix]
}

// genHelpers emits the helpers this file uses. A helper may use others, so
// generation repeats until no new helper is used.
func (fg *fileGen) genHelpers() {
	code := map[string]string{}
	for {
		added := false
		for _, h := range helperDefs {
			name, ok := fg.funcs[h.suffix]
			if !ok || code[h.suffix] != "" {
				continue
			}
			code[h.suffix] = fg.capture(func() {
				if fg.shared {
					fg.P("// ", h.doc)
				}
				h.gen(fg, name)
			})
			added = true
		}
		if !added {
			break
		}
	}
	for _, h := range helperDefs {
		fg.buf.WriteString(code[h.suffix])
	}
}

// jsonErrorf returns an expression for the error "proto: <name>: <msg>"
// followed by the Go string expressions parts, where name is a Go string
// expression. msg becomes a constant if it is used more than once.
func (fg *fileGen) jsonErrorf(name, hint, msg string, parts ...string) string {
	e := fg.fn("JSONError") + "(" + name + ", " + fg.errString(hint, msg)
	for _, p := range parts {
		e += " + " + p
	}
	return e + ")"
}

// Type parameter constraints shared by several helpers.
const (
	varintTypes = "~int | ~int32 | ~int64 | ~uint32 | ~uint64"
	bytesTypes  = "~string | ~[]byte"
)

// jt returns the jsontext package name.
func (fg *fileGen) jt() string { return fg.std("encoding/json/jsontext") }

// --- Binary sizing and writing ----------------------------------------------

func genSizeLen(fg *fileGen, name string) {
	fg.P("func ", name, "(l int) int { return l + ", fg.fn("VarintLen"), "(l) }")
}

func genSizeVarint(fg *fileGen, name string) {
	fg.P("func ", name, "(tag int, u uint64) int { return ", fg.fn("If"), "(u == 0, 0, tag+", fg.fn("VarintLen"), "(u)) }")
}

func genSizeBool(fg *fileGen, name string) {
	fg.P("func ", name, "(tag int, v bool) int { return ", fg.fn("If"), "(v, tag+1, 0) }")
}

func genSizeFixed(fg *fileGen, name string) {
	fg.P("func ", name, "(tag, size int, set bool) int { return ", fg.fn("If"), "(set, tag+size, 0) }")
}

func genSizeLenField(fg *fileGen, name string) {
	fg.P("func ", name, "(tag, l int) int { return ", fg.fn("If"), "(l == 0, 0, tag+", fg.fn("SizeLen"), "(l)) }")
}

func genSizePresentBytes(fg *fileGen, name string) {
	fg.P("func ", name, "(tag int, v []byte) int { return ", fg.fn("If"), "(v == nil, 0, tag+", fg.fn("SizeLen"), "(len(v))) }")
}

func genSizeMsg(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](tag int, p *T, size func(*T) int) int {")
	fg.P("if p == nil {")
	fg.P("return 0")
	fg.P("}")
	fg.P("return tag + ", fg.fn("SizeLen"), "(size(p))")
	fg.P("}")
}

func genSizeMsgs(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](tag int, s []*T, size func(*T) int) (n int) {")
	fg.P("for _, v := range s {")
	fg.P("n += tag + ", fg.fn("SizeLen"), "(size(v))")
	fg.P("}")
	fg.P("return n")
	fg.P("}")
}

func genSizeOpt(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](tag int, p *T, size func(T) int) int {")
	fg.P("if p == nil {")
	fg.P("return 0")
	fg.P("}")
	fg.P("return tag + size(*p)")
	fg.P("}")
}

func genSizeOptFixed(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](tag, size int, p *T) int { return ", fg.fn("If"), "(p == nil, 0, tag+size) }")
}

func genSizePacked(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](tag int, s []T, size func(T) int) int {")
	fg.P("if len(s) == 0 {")
	fg.P("return 0")
	fg.P("}")
	fg.P("l := 0")
	fg.P("for _, v := range s {")
	fg.P("l += size(v)")
	fg.P("}")
	fg.P("return tag + ", fg.fn("SizeLen"), "(l)")
	fg.P("}")
}

func genSizeEach(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](tag int, s []T, size func(T) int) (n int) {")
	fg.P("for _, v := range s {")
	fg.P("n += tag + size(v)")
	fg.P("}")
	fg.P("return n")
	fg.P("}")
}

func genVarintLen(fg *fileGen, name string) {
	fg.P("func ", name, "[T ", varintTypes, "](v T) int { return ", fg.sizeVarint("uint64(int64(v))"), " }")
}

func genSint32Len(fg *fileGen, name string) {
	fg.P("func ", name, "(v int32) int { return ", fg.sizeVarint(varintExpr(desc.KindSint32, "v")), " }")
}

func genSint64Len(fg *fileGen, name string) {
	fg.P("func ", name, "(v int64) int { return ", fg.sizeVarint(varintExpr(desc.KindSint64, "v")), " }")
}

func genBoolLen(fg *fileGen, name string) {
	fg.P("func ", name, "(bool) int { return 1 }")
}

func genBytesLen(fg *fileGen, name string) {
	fg.P("func ", name, "[T ", bytesTypes, "](v T) int { return ", fg.fn("SizeLen"), "(len(v)) }")
}

func genCheckUTF8(fg *fileGen, name string) {
	fg.P("func ", name, "(s, msg string, err error) error {")
	fg.P("if len(s) > 0 && !", fg.std("unicode/utf8"), ".ValidString(s) {")
	fg.P("return ", fg.std("errors"), ".New(msg)")
	fg.P("}")
	fg.P("return err")
	fg.P("}")
}

func genCopyUnknown(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, i int, u []byte) int {")
	// Guarded, because copy is not free when there is nothing to copy.
	fg.P("if len(u) > 0 {")
	fg.P("i -= copy(b[i-len(u):], u)")
	fg.P("}")
	fg.P("return i")
	fg.P("}")
}

// AppendBinary grows b by size, marshals into the new space and checks the
// count. marshal is a method value of the message; it does not escape.
func genAppendBinary(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, size int, marshal func([]byte) (int, error)) ([]byte, error) {")
	fg.P("b = ", fg.std("slices"), ".Grow(b, size)")
	fg.P("n, err := marshal(b[len(b) : len(b)+size])")
	fg.P("if err == nil && n != size {")
	fg.P("err = ", fg.errExpr("ErrSizeChanged", errSizeMsg))
	fg.P("}")
	fg.P("if err != nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("return b[:len(b)+size], nil")
	fg.P("}")
}

func genPutVarint(fg *fileGen, name string) {
	pb := fg.c("VarintPayloadBits")
	fg.P("func ", name, "(b []byte, i int, u uint64) int {")
	fg.P("if u < ", fg.c("VarintContBit"), " {")
	fg.P("b[i-1] = byte(u)")
	fg.P("return i - 1")
	fg.P("}")
	fg.P("i -= (", fg.std("math/bits"), ".Len64(u|1) + ", pb, " - 1) / ", pb)
	fg.P(fg.std("encoding/binary"), ".PutUvarint(b[i:], u)")
	fg.P("return i")
	fg.P("}")
}

func genPutFixed32(fg *fileGen, name string) {
	fs := fg.c("Fixed32Size")
	fg.P("func ", name, "(b []byte, i int, u uint32) int {")
	fg.P(fg.std("encoding/binary"), ".LittleEndian.PutUint32(b[i-", fs, ":], u)")
	fg.P("return i - ", fs)
	fg.P("}")
}

func genPutFixed64(fg *fileGen, name string) {
	fs := fg.c("Fixed64Size")
	fg.P("func ", name, "(b []byte, i int, u uint64) int {")
	fg.P(fg.std("encoding/binary"), ".LittleEndian.PutUint64(b[i-", fs, ":], u)")
	fg.P("return i - ", fs)
	fg.P("}")
}

func genPutBool(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, i int, v bool) int {")
	fg.P("b[i-1] = 0")
	fg.P("if v {")
	fg.P("b[i-1] = 1")
	fg.P("}")
	fg.P("return i - 1")
	fg.P("}")
}

// The field writers write an implicit-presence value with its tag, or
// nothing if the value is zero, in one call that the compiler inlines. The
// tag is one byte (field numbers below 16) or, for the "2" variants, two
// bytes; strings and bytes with two-byte tags are written inline, because
// that writer would not be inlined. The varint is written with a loop
// rather than binary.PutUvarint, whose call would exceed the inlining
// budget.

// putTag emits the statements writing tag at b[i], for a one-byte tag or a
// two-byte one (two).
func putTag(fg *fileGen, two bool) {
	if two {
		fg.P("b[i], b[i+1] = byte(tag)|", fg.c("VarintContBit"), ", byte(tag>>", fg.c("VarintPayloadBits"), ")")
	} else {
		fg.P("b[i] = tag")
	}
}

func tagType(two bool) string {
	if two {
		return "uint16"
	}
	return "byte"
}

func genPutVarintFieldN(fg *fileGen, name string, two bool) {
	pb, cb := fg.c("VarintPayloadBits"), fg.c("VarintContBit")
	fg.P("func ", name, "(b []byte, i int, u uint64, tag ", tagType(two), ") int {")
	fg.P("if u == 0 {")
	fg.P("return i")
	fg.P("}")
	fg.P("i -= (", fg.std("math/bits"), ".Len64(u|1)+", pb, "-1)/", pb, " + ", map[bool]int{false: 1, true: 2}[two])
	putTag(fg, two)
	fg.P("p := i + ", map[bool]int{false: 1, true: 2}[two])
	fg.P("for ; u >= ", cb, "; u >>= ", pb, " {")
	fg.P("b[p] = byte(u) | ", cb)
	fg.P("p++")
	fg.P("}")
	fg.P("b[p] = byte(u)")
	fg.P("return i")
	fg.P("}")
}

func genPutVarintField(fg *fileGen, name string)  { genPutVarintFieldN(fg, name, false) }
func genPutVarintField2(fg *fileGen, name string) { genPutVarintFieldN(fg, name, true) }

// genPutLenField emits a writer of a string or bytes field with a one-byte
// tag: the value is copied, then its length and the tag are written like a
// varint field.
func genPutLenField(fg *fileGen, name, typ string) {
	pb, cb := fg.c("VarintPayloadBits"), fg.c("VarintContBit")
	fg.P("func ", name, "(b []byte, i int, v ", typ, ", tag byte) int {")
	fg.P("if len(v) == 0 {")
	fg.P("return i")
	fg.P("}")
	fg.P("i -= copy(b[i-len(v):], v)")
	fg.P("u := uint64(len(v))")
	fg.P("i -= (", fg.std("math/bits"), ".Len64(u|1)+", pb, "-1)/", pb, " + 1")
	fg.P("b[i] = tag")
	fg.P("p := i + 1")
	fg.P("for ; u >= ", cb, "; u >>= ", pb, " {")
	fg.P("b[p] = byte(u) | ", cb)
	fg.P("p++")
	fg.P("}")
	fg.P("b[p] = byte(u)")
	fg.P("return i")
	fg.P("}")
}

func genPutStringField(fg *fileGen, name string) { genPutLenField(fg, name, "string") }
func genPutBytesField(fg *fileGen, name string)  { genPutLenField(fg, name, "[]byte") }

func genPutFixedFieldN(fg *fileGen, name string, bits int, two bool) {
	size := fg.c("Fixed" + map[int]string{32: "32", 64: "64"}[bits] + "Size")
	tn := map[bool]int{false: 1, true: 2}[two]
	fg.P("func ", name, "(b []byte, i int, u uint", bits, ", tag ", tagType(two), ") int {")
	fg.P("if u == 0 {")
	fg.P("return i")
	fg.P("}")
	fg.P(fg.std("encoding/binary"), ".LittleEndian.PutUint", bits, "(b[i-", size, ":], u)")
	fg.P("i -= ", size, " + ", tn)
	putTag(fg, two)
	fg.P("return i")
	fg.P("}")
}

func genPutFixed32Field(fg *fileGen, name string)  { genPutFixedFieldN(fg, name, 32, false) }
func genPutFixed64Field(fg *fileGen, name string)  { genPutFixedFieldN(fg, name, 64, false) }
func genPutFixed32Field2(fg *fileGen, name string) { genPutFixedFieldN(fg, name, 32, true) }
func genPutFixed64Field2(fg *fileGen, name string) { genPutFixedFieldN(fg, name, 64, true) }

func genPutBoolFieldN(fg *fileGen, name string, two bool) {
	tn := map[bool]int{false: 1, true: 2}[two]
	fg.P("func ", name, "(b []byte, i int, v bool, tag ", tagType(two), ") int {")
	fg.P("if !v {")
	fg.P("return i")
	fg.P("}")
	fg.P("i -= ", tn+1)
	putTag(fg, two)
	fg.P("b[i+", tn, "] = 1")
	fg.P("return i")
	fg.P("}")
}

func genPutBoolField(fg *fileGen, name string)  { genPutBoolFieldN(fg, name, false) }
func genPutBoolField2(fg *fileGen, name string) { genPutBoolFieldN(fg, name, true) }

func genSortedKeys(fg *fileGen, name string) {
	fg.P("func ", name, "[K ", fg.std("cmp"), ".Ordered, V any](m map[K]V, keys []K) []K {")
	fg.P("for k := range m {")
	fg.P("keys = append(keys, k)")
	fg.P("}")
	fg.P(fg.std("slices"), ".Sort(keys)")
	fg.P("return keys")
	fg.P("}")
}

// If is a conditional expression, which lets other helpers fit on one line.
// Both alternatives are evaluated, so they must be cheap and safe.
func genIf(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](c bool, a, b T) T {")
	fg.P("if c {")
	fg.P("return a")
	fg.P("}")
	fg.P("return b")
	fg.P("}")
}

// --- Binary reading -----------------------------------------------------------

// The decoders take the buffer positioned after the tag and a pointer to the
// destination, and return the number of bytes consumed, which is zero or
// negative when the value is malformed. String reports invalid UTF-8 as an
// error. The message loop checks the count and the error after its switch.

func genDepth(fg *fileGen, name string) {
	fg.P("func ", name, "(depth int) error {")
	fg.P("if depth >= ", fg.c("MaxDepth"), " {")
	fg.P("return ", fg.errExpr("ErrDepth", errDepthMsg))
	fg.P("}")
	fg.P("return nil")
	fg.P("}")
}

func genReadBytes(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte) (v []byte, n int) {")
	fg.P("ln, k := ", fg.std("encoding/binary"), ".Uvarint(b)")
	fg.P("if k <= 0 || ln > uint64(len(b)-k) {")
	fg.P("return nil, -1")
	fg.P("}")
	fg.P("return b[k : k+int(ln)], k + int(ln)")
	fg.P("}")
}

// SkipField is the only check of a tag's field number: known fields are
// matched by their whole tags.
func genSkipField(fg *fileGen, name string) {
	tb := fg.c("TagTypeBits")
	fg.P("func ", name, "(b []byte, t uint64, depth int) (int, error) {")
	fg.P("n, num := 0, t>>", tb)
	fg.P("switch wt := t & ", fg.c("TagTypeMask"), "; {")
	fg.P("case num == 0 || num > ", fg.c("MaxFieldNumber"), ":")
	fg.P("case wt == ", fg.wire(wireVarint), ":")
	fg.P("_, n = ", fg.std("encoding/binary"), ".Uvarint(b)")
	fg.P("case wt == ", fg.wire(wireBytes), ":")
	fg.P("_, n = ", fg.fn("ReadBytes"), "(b)")
	fg.P("case wt == ", fg.wire(wireStartGroup), ":")
	fg.P("return ", fg.fn("SkipGroup"), "(b, int32(num), depth+1)")
	fg.P("case wt == ", fg.wire(wireFixed64), " || wt == ", fg.wire(wireFixed32), ":")
	fg.P("n = ", fg.fn("If"), "(wt == ", fg.wire(wireFixed64), ", ", fg.c("Fixed64Size"), ", ", fg.c("Fixed32Size"), ")")
	fg.P("n = ", fg.fn("If"), "(len(b) >= n, n, 0)")
	fg.P("}")
	fg.P("if n <= 0 {")
	fg.P("return 0, ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("}")
	fg.P("return n, nil")
	fg.P("}")
}

// SkipGroup skips the fields of the group with number num up to its end
// tag, recursing through SkipField for nested groups, and returns the
// length skipped.
func genSkipGroup(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, num int32, depth int) (int, error) {")
	fg.P("n, err := 0, ", fg.fn("Depth"), "(depth)")
	fg.P("for err == nil {")
	fg.P("t, k := ", fg.std("encoding/binary"), ".Uvarint(b[n:])")
	fg.P("if k <= 0 {")
	fg.P("return 0, ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("}")
	fg.P("if n += k; t == ", fg.tagExpr("uint64(num)", wireEndGroup), " {")
	fg.P("return n, nil")
	fg.P("}")
	fg.P("k, err = ", fg.fn("SkipField"), "(b[n:], t, depth)")
	fg.P("n += k")
	fg.P("}")
	fg.P("return 0, err")
	fg.P("}")
}

// Unknown skips the field with tag t at b and keeps it, with a canonical
// tag, as an unknown field.
func genUnknown(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, t uint64, depth int, unk *[]byte) (int, error) {")
	fg.P("n, err := ", fg.fn("SkipField"), "(b, t, depth)")
	fg.P("if err == nil {")
	fg.P("*unk = append(", fg.std("encoding/binary"), ".AppendUvarint(*unk, t), b[:n]...)")
	fg.P("}")
	fg.P("return n, err")
	fg.P("}")
}

// End reports err, or a parse error if b was not consumed entirely.
func genEnd(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, err error) error {")
	fg.P("if err == nil && len(b) > 0 {")
	fg.P("return ", fg.errExpr("ErrParse", errParseMsg))
	fg.P("}")
	fg.P("return err")
	fg.P("}")
}

// genVarintDecoder emits a decoder of a varint kind: dst is the assignment
// of the decoded value u to *p.
func genVarintDecoder(fg *fileGen, name, typeParams, ptype, dst string) {
	fg.P("func ", name, typeParams, "(b []byte, p *", ptype, ") int { u, n := ", fg.std("encoding/binary"), ".Uvarint(b); *p = ", dst, "; return n }")
}

func genVarint(fg *fileGen, name string) {
	genVarintDecoder(fg, name, "[T "+varintTypes+"]", "T", "T(u)")
}

func genSint32(fg *fileGen, name string) {
	genVarintDecoder(fg, name, "", "int32", "int32(uint32(u)>>1) ^ -int32(u&1)")
}

func genSint64(fg *fileGen, name string) {
	genVarintDecoder(fg, name, "", "int64", "int64(u>>1) ^ -int64(u&1)")
}

func genBool(fg *fileGen, name string) {
	genVarintDecoder(fg, name, "", "bool", "u != 0")
}

// genFixedDecoder emits a decoder of a fixed-width kind: dst converts the
// little-endian value read by read.
func genFixedDecoder(fg *fileGen, name, typeParams, ptype, size, dst string) {
	fg.P("func ", name, typeParams, "(b []byte, p *", ptype, ") int {")
	fg.P("if len(b) < ", size, " {")
	fg.P("return -1")
	fg.P("}")
	fg.P("*p = ", dst)
	fg.P("return ", size)
	fg.P("}")
}

func genFixed32(fg *fileGen, name string) {
	genFixedDecoder(fg, name, "[T ~uint32 | ~int32]", "T", fg.c("Fixed32Size"), "T("+fg.std("encoding/binary")+".LittleEndian.Uint32(b))")
}

func genFixed64(fg *fileGen, name string) {
	genFixedDecoder(fg, name, "[T ~uint64 | ~int64]", "T", fg.c("Fixed64Size"), "T("+fg.std("encoding/binary")+".LittleEndian.Uint64(b))")
}

func genFloat(fg *fileGen, name string) {
	genFixedDecoder(fg, name, "", "float32", fg.c("Fixed32Size"), fg.std("math")+".Float32frombits("+fg.std("encoding/binary")+".LittleEndian.Uint32(b))")
}

func genDouble(fg *fileGen, name string) {
	genFixedDecoder(fg, name, "", "float64", fg.c("Fixed64Size"), fg.std("math")+".Float64frombits("+fg.std("encoding/binary")+".LittleEndian.Uint64(b))")
}

func genBytes(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, p *[]byte) int {")
	fg.P("ln, k := ", fg.std("encoding/binary"), ".Uvarint(b)")
	fg.P("if k <= 0 || ln > uint64(len(b)-k) {")
	fg.P("return -1")
	fg.P("}")
	fg.P("*p = append([]byte{}, b[k:k+int(ln)]...)")
	fg.P("return k + int(ln)")
	fg.P("}")
}

func genString(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, p *string, errUTF8 string) (int, error) {")
	fg.P("ln, k := ", fg.std("encoding/binary"), ".Uvarint(b)")
	fg.P("if k <= 0 || ln > uint64(len(b)-k) {")
	fg.P("return -1, nil")
	fg.P("}")
	fg.P("x := b[k : k+int(ln)]")
	fg.P("if errUTF8 != \"\" && !", fg.std("unicode/utf8"), ".Valid(x) {")
	fg.P("return 0, ", fg.std("errors"), ".New(errUTF8)")
	fg.P("}")
	fg.P("*p = string(x)")
	fg.P("return k + int(ln), nil")
	fg.P("}")
}

// genPackedVarintDecoder emits a decoder of a packed field of a varint kind.
func genPackedVarintDecoder(fg *fileGen, name, typeParams, etype, elem string) {
	fg.P("func ", name, typeParams, "(b []byte, s *[]", etype, ") int {")
	fg.P("x, n := ", fg.fn("ReadBytes"), "(b)")
	fg.P("if n < 0 {")
	fg.P("return -1")
	fg.P("}")
	fg.P("for len(x) > 0 {")
	fg.P("u, k := ", fg.std("encoding/binary"), ".Uvarint(x)")
	fg.P("if k <= 0 {")
	fg.P("return -1")
	fg.P("}")
	fg.P("x, *s = x[k:], append(*s, ", elem, ")")
	fg.P("}")
	fg.P("return n")
	fg.P("}")
}

func genPackedVarint(fg *fileGen, name string) {
	genPackedVarintDecoder(fg, name, "[T "+varintTypes+"]", "T", "T(u)")
}

func genPackedSint32(fg *fileGen, name string) {
	genPackedVarintDecoder(fg, name, "", "int32", "int32(uint32(u)>>1)^-int32(u&1)")
}

func genPackedSint64(fg *fileGen, name string) {
	genPackedVarintDecoder(fg, name, "", "int64", "int64(u>>1)^-int64(u&1)")
}

func genPackedBool(fg *fileGen, name string) {
	genPackedVarintDecoder(fg, name, "", "bool", "u != 0")
}

// genPackedFixedDecoder emits a decoder of a packed field of a fixed-width
// kind, which allocates the slice for the whole field at once.
func genPackedFixedDecoder(fg *fileGen, name, typeParams, etype, size, elem string) {
	fg.P("func ", name, typeParams, "(b []byte, s *[]", etype, ") int {")
	fg.P("x, n := ", fg.fn("ReadBytes"), "(b)")
	fg.P("if n < 0 || len(x)%", size, " != 0 {")
	fg.P("return -1")
	fg.P("}")
	fg.P("if *s == nil {")
	fg.P("*s = make([]", etype, ", 0, len(x)/", size, ")")
	fg.P("}")
	fg.P("for ; len(x) > 0; x = x[", size, ":] {")
	fg.P("*s = append(*s, ", elem, ")")
	fg.P("}")
	fg.P("return n")
	fg.P("}")
}

func genPackedFixed32(fg *fileGen, name string) {
	genPackedFixedDecoder(fg, name, "[T ~uint32 | ~int32]", "T", fg.c("Fixed32Size"), "T("+fg.std("encoding/binary")+".LittleEndian.Uint32(x))")
}

func genPackedFixed64(fg *fileGen, name string) {
	genPackedFixedDecoder(fg, name, "[T ~uint64 | ~int64]", "T", fg.c("Fixed64Size"), "T("+fg.std("encoding/binary")+".LittleEndian.Uint64(x))")
}

func genPackedFloat(fg *fileGen, name string) {
	genPackedFixedDecoder(fg, name, "", "float32", fg.c("Fixed32Size"), fg.std("math")+".Float32frombits("+fg.std("encoding/binary")+".LittleEndian.Uint32(x))")
}

func genPackedDouble(fg *fileGen, name string) {
	genPackedFixedDecoder(fg, name, "", "float64", fg.c("Fixed64Size"), fg.std("math")+".Float64frombits("+fg.std("encoding/binary")+".LittleEndian.Uint64(x))")
}

// The closed enum decoders keep unrecognized values as unknown fields, and
// call set with recognized ones, so that the caller decides where to store
// them.
func genClosedEnum(fg *fileGen, name string) {
	bin := fg.std("encoding/binary")
	fg.P("func ", name, "[E interface {")
	fg.P("~int32")
	fg.P("IsValid() bool")
	fg.P("}](b []byte, unk *[]byte, tag uint64, set func(E)) int {")
	fg.P("u, n := ", bin, ".Uvarint(b)")
	fg.P("if n <= 0 {")
	fg.P("return -1")
	fg.P("}")
	fg.P("if e := E(u); e.IsValid() {")
	fg.P("set(e)")
	fg.P("} else {")
	fg.P("*unk = append(", bin, ".AppendUvarint(*unk, tag), b[:n]...)")
	fg.P("}")
	fg.P("return n")
	fg.P("}")
}

func genPackedClosedEnum(fg *fileGen, name string) {
	fg.P("func ", name, "[E interface {")
	fg.P("~int32")
	fg.P("IsValid() bool")
	fg.P("}](b []byte, unk *[]byte, tag uint64, set func(E)) int {")
	fg.P("x, n := ", fg.fn("ReadBytes"), "(b)")
	fg.P("if n < 0 {")
	fg.P("return -1")
	fg.P("}")
	fg.P("for len(x) > 0 {")
	fg.P("k := ", fg.fn("ClosedEnum"), "(x, unk, tag, set)")
	fg.P("if k <= 0 {")
	fg.P("return -1")
	fg.P("}")
	fg.P("x = x[k:]")
	fg.P("}")
	fg.P("return n")
	fg.P("}")
}

// Group returns the contents of the group starting at b, without its end
// tag, and the length of the whole group.
func genGroup(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, num int32, depth int) ([]byte, int, error) {")
	fg.P("n, err := ", fg.fn("SkipGroup"), "(b, num, depth)")
	fg.P("if err != nil {")
	fg.P("return nil, 0, err")
	fg.P("}")
	fg.P("return b[:n-", fg.sizeVarint("uint64(num)<<"+fg.c("TagTypeBits")+"|"+fg.wire(wireEndGroup)), "], n, nil")
	fg.P("}")
}

func genNew(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](p **T) *T { *p = new(T); return *p }")
}

func genGrow(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](s *[]T) *T { var z T; *s = append(*s, z); return &(*s)[len(*s)-1] }")
}

func genAlloc(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](p **T) *T {")
	fg.P("if *p == nil {")
	fg.P("*p = new(T)")
	fg.P("}")
	fg.P("return *p")
	fg.P("}")
}

func genMapSet(fg *fileGen, name string) {
	fg.P("func ", name, "[K comparable, V any](m *map[K]V, k K, v V) {")
	fg.P("if *m == nil {")
	fg.P("*m = make(map[K]V)")
	fg.P("}")
	fg.P("(*m)[k] = v")
	fg.P("}")
}

// --- Enums and getters ----------------------------------------------------------

func genInvert(fg *fileGen, name string) {
	fg.P("func ", name, "[K, V comparable](m map[K]V) map[V]K {")
	fg.P("r := make(map[V]K, len(m))")
	fg.P("for k, v := range m {")
	fg.P("r[v] = k")
	fg.P("}")
	fg.P("return r")
	fg.P("}")
}

func genEnumString(fg *fileGen, name string) {
	fg.P("func ", name, "(names map[int32]string, v int32) string {")
	fg.P("if s, ok := names[v]; ok {")
	fg.P("return s")
	fg.P("}")
	fg.P("return ", fg.std("strconv"), ".Itoa(int(v))")
	fg.P("}")
}

func genDeref(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](p *T, def T) T {")
	fg.P("if p != nil {")
	fg.P("return *p")
	fg.P("}")
	fg.P("return def")
	fg.P("}")
}

func genBytesOr(fg *fileGen, name string) {
	fg.P("func ", name, "(v, def []byte) []byte {")
	fg.P("if v != nil {")
	fg.P("return v")
	fg.P("}")
	fg.P("return append([]byte(nil), def...)")
	fg.P("}")
}

// --- JSON marshal -----------------------------------------------------------

// MarshalTo writes the output of f, a ProtoAppendJSON method value, to e.
func genMarshalTo(fg *fileGen, name string) {
	fg.P("func ", name, "(e *", fg.jt(), ".Encoder, f func([]byte) ([]byte, error)) error {")
	fg.P("b, err := f(e.AvailableBuffer())")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("return e.WriteValue(b)")
	fg.P("}")
}

func genAppendEnum(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, v int32, names map[int32]string) []byte {")
	fg.P("if s, ok := names[v]; ok {")
	fg.P("b = append(b, '\"')")
	fg.P("b = append(b, s...)")
	fg.P("return append(b, '\"')")
	fg.P("}")
	fg.P("return ", fg.std("strconv"), ".AppendInt(b, int64(v), 10)")
	fg.P("}")
}

func genAppendFloat(fg *fileGen, name string) {
	m := fg.std("math")
	fg.P("func ", name, "(b []byte, f float64, bits int) []byte {")
	fg.P("switch {")
	fg.P("case ", m, ".IsNaN(f):")
	fg.P("return append(b, `\"NaN\"`...)")
	fg.P("case ", m, ".IsInf(f, 1):")
	fg.P("return append(b, `\"Infinity\"`...)")
	fg.P("case ", m, ".IsInf(f, -1):")
	fg.P("return append(b, `\"-Infinity\"`...)")
	fg.P("}")
	fg.P("return ", fg.jt(), ".AppendFloat(b, f, bits)")
	fg.P("}")
}

// The JSON append helpers take the error so far and do nothing once it is
// set, so that a method appends every member with one statement and
// returns the first error at the end.
func genQuote(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, s string, err error) ([]byte, error) {")
	fg.P("if err != nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("return ", fg.jt(), ".AppendQuote(b, s)")
	fg.P("}")
}

func genAppendMsg(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](b []byte, lit string, p *T, f func(*T, []byte) ([]byte, error), err error) ([]byte, error) {")
	fg.P("if err != nil || p == nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("return f(p, append(b, lit...))")
	fg.P("}")
}

// AppendList writes a JSON array of the elements of s after lit, which ends
// with the opening bracket, and nothing if s is empty.
func genAppendList(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](b []byte, lit string, s []T, f func(T, []byte) ([]byte, error), err error) ([]byte, error) {")
	fg.P("if err != nil || len(s) == 0 {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("b = append(b, lit...)")
	fg.P("for _, v := range s {")
	fg.P("if b, err = f(v, b); err != nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("b = append(b, ',')")
	fg.P("}")
	fg.P("b[len(b)-1] = ']'")
	fg.P("return b, nil")
	fg.P("}")
}

// AppendMap writes a JSON object of the entries of m in key order after lit,
// which ends with the opening brace, and nothing if m is empty.
func genAppendMap(fg *fileGen, name string) {
	fg.P("func ", name, "[K ", fg.std("cmp"), ".Ordered, V any](b []byte, lit string, m map[K]V, fk func(K, []byte) ([]byte, error), fv func(V, []byte) ([]byte, error), err error) ([]byte, error) {")
	fg.P("if err != nil || len(m) == 0 {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("b = append(b, lit...)")
	fg.P("for _, k := range ", fg.fn("SortedKeys"), "(m, make([]K, 0, len(m))) {")
	fg.P("if b, err = fk(k, b); err != nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("if b, err = fv(m[k], append(b, ':')); err != nil {")
	fg.P("return b, err")
	fg.P("}")
	fg.P("b = append(b, ',')")
	fg.P("}")
	fg.P("b[len(b)-1] = '}'")
	fg.P("return b, nil")
	fg.P("}")
}

func genCloseObject(fg *fileGen, name string) {
	fg.P("func ", name, "(b []byte, start int) []byte {")
	fg.P("if len(b) == start {")
	fg.P("return append(b, \"{}\"...)")
	fg.P("}")
	fg.P("b[start] = '{'")
	fg.P("return append(b, '}')")
	fg.P("}")
}

// --- JSON unmarshal ---------------------------------------------------------

// MergeJSON decodes b with merge, a ProtoMergeJSONFrom method value, and
// rejects trailing data.
func genMergeJSON(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(b []byte, name string, merge func(*", jt, ".Decoder) error) error {")
	fg.P("d := ", jt, ".NewDecoder(", fg.std("bytes"), ".NewBuffer(b))")
	fg.P("if err := merge(d); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if _, err := d.ReadToken(); err != ", fg.std("io"), ".EOF {")
	fg.P("return ", fg.jsonErrorf("name", "ErrTrailingData", "unexpected data after JSON value"))
	fg.P("}")
	fg.P("return nil")
	fg.P("}")
}

// MergeFrom decodes from d, or from a strict copy of its next value, with
// merge, a ProtoMergeJSONFrom method value.
func genMergeFrom(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(d *", jt, ".Decoder, merge func(*", jt, ".Decoder) error) error {")
	fg.P("d, err := ", fg.fn("StrictDecoder"), "(d)")
	fg.P("return ", fg.std("cmp"), ".Or(err, merge(d))")
	fg.P("}")
}

// StrictDecoder returns d, or a decoder over d's next value that rejects
// invalid UTF-8 if d does not.
func genStrictDecoder(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(d *", jt, ".Decoder) (*", jt, ".Decoder, error) {")
	fg.P("if lax, _ := ", fg.std("encoding/json/v2"), ".GetOption(d.Options(), ", jt, ".AllowInvalidUTF8); !lax {")
	fg.P("return d, nil")
	fg.P("}")
	fg.P("v, err := d.ReadValue()")
	fg.P("if err != nil {")
	fg.P("return d, err")
	fg.P("}")
	fg.P("return ", jt, ".NewDecoder(", fg.std("bytes"), ".NewBuffer(v)), nil")
	fg.P("}")
}

// OpenObject reads the opening brace of the object of message name and its
// first key, which is EndObject for an empty object and for null.
func genOpenObject(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(d *", jt, ".Decoder, name string) (", jt, ".Token, error) {")
	fg.P("if d.PeekKind() == ", fg.kind("Null"), " {")
	fg.P("return ", jt, ".EndObject, d.SkipValue()")
	fg.P("}")
	fg.P("return ", fg.fn("NextKey"), "(d, ", fg.fn("ExpectJSON"), "(d, ", fg.kind("BeginObject"), ", name, \"object\"))")
	fg.P("}")
}

func genExpectJSON(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(d *", jt, ".Decoder, kind ", jt, ".Kind, name, what string) error {")
	fg.P("tok, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if tok.Kind() != kind {")
	fg.P("return ", fg.jsonErrorf("name", "ErrExpected", "expected a JSON ", "what"))
	fg.P("}")
	fg.P("return nil")
	fg.P("}")
}

// NextKey reads the next key of an object, or its closing brace, unless
// err is set. The message loop ends on EndObject and returns any error
// through an invalid token.
func genNextKey(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(d *", jt, ".Decoder, err error) (", jt, ".Token, error) {")
	fg.P("if err != nil {")
	fg.P("return ", jt, ".Token{}, err")
	fg.P("}")
	fg.P("return d.ReadToken()")
	fg.P("}")
}

// parseFuncType is the type of the functions that read one JSON value into a
// destination: the scalar parsers below, JSONMsg, and the per-enum functions
// declared by enumJSONFunc.
func (fg *fileGen) parseFuncType(t string) string {
	return "func(*" + fg.jt() + ".Decoder, *" + t + ", string) error"
}

// genFieldPrologue emits the duplicate check shared by the field helpers.
func genFieldPrologue(fg *fileGen) {
	fg.P("if *seen {")
	fg.P("return ", fg.jsonErrorf("name", "ErrDuplicateField", "duplicate field ", fg.std("strconv")+".Quote(key)"))
	fg.P("}")
	fg.P("*seen = true")
}

// genNullSkip emits code leaving the destination unchanged on JSON null.
func genNullSkip(fg *fileGen) {
	fg.P("if d.PeekKind() == ", fg.kind("Null"), " {")
	fg.P("return d.SkipValue()")
	fg.P("}")
}

func genField(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](d *", fg.jt(), ".Decoder, seen *bool, key, name string, p *T, parse ", fg.parseFuncType("T"), ") error {")
	genFieldPrologue(fg)
	fg.P("return parse(d, p, name)")
	fg.P("}")
}

func genOptField(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](d *", fg.jt(), ".Decoder, seen *bool, key, name string, p **T, parse ", fg.parseFuncType("T"), ") error {")
	genFieldPrologue(fg)
	genNullSkip(fg)
	fg.P("return parse(d, ", fg.fn("New"), "(p), name)")
	fg.P("}")
}

func genRepField(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](d *", fg.jt(), ".Decoder, seen *bool, key, name string, s *[]T, nullOK bool, parse ", fg.parseFuncType("T"), ") error {")
	genFieldPrologue(fg)
	genNullSkip(fg)
	fg.P("return ", fg.fn("JSONArray"), "(d, s, name, nullOK, parse)")
	fg.P("}")
}

func genMapField(fg *fileGen, name string) {
	fg.P("func ", name, "[K comparable, V any](d *", fg.jt(), ".Decoder, seen *bool, key, name, field string, m *map[K]V, nullOK bool, pk func(string, string, string) (K, error), pv ", fg.parseFuncType("V"), ") error {")
	genFieldPrologue(fg)
	genNullSkip(fg)
	fg.P("return ", fg.fn("JSONMap"), "(d, m, name, field, nullOK, pk, pv)")
	fg.P("}")
}

// OneofField stores wrapper w in the oneof field p and parses into w's
// field v, unless the value is null and null is not a value (nullOK).
func genOneofField(fg *fileGen, name string) {
	fg.P("func ", name, "[I, T any](d *", fg.jt(), ".Decoder, seen, oneof *bool, key, name, oneofName string, p *I, w I, v *T, nullOK bool, parse ", fg.parseFuncType("T"), ") error {")
	genFieldPrologue(fg)
	fg.P("if !nullOK && d.PeekKind() == ", fg.kind("Null"), " {")
	fg.P("return d.SkipValue()")
	fg.P("}")
	fg.P("if *oneof {")
	fg.P("return ", fg.jsonErrorf("name", "ErrOneofConflict", "multiple fields set for oneof ", "oneofName"))
	fg.P("}")
	fg.P("*oneof, *p = true, w")
	fg.P("return parse(d, v, name)")
	fg.P("}")
}

const errNullElement = "null is not allowed in repeated fields or map values"

func genJSONArray(fg *fileGen, name string) {
	fg.P("func ", name, "[T any](d *", fg.jt(), ".Decoder, s *[]T, name string, nullOK bool, parse ", fg.parseFuncType("T"), ") error {")
	fg.P("if err := ", fg.fn("ExpectJSON"), "(d, ", fg.kind("BeginArray"), ", name, \"array\"); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("for d.PeekKind() != ", fg.kind("EndArray"), " {")
	fg.P("if !nullOK && d.PeekKind() == ", fg.kind("Null"), " {")
	fg.P("return ", fg.jsonErrorf("name", "ErrNullElement", errNullElement))
	fg.P("}")
	fg.P("if err := parse(d, ", fg.fn("Grow"), "(s), name); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("}")
	fg.P("_, err := d.ReadToken()")
	fg.P("return err")
	fg.P("}")
}

func genJSONMap(fg *fileGen, name string) {
	fg.P("func ", name, "[K comparable, V any](d *", fg.jt(), ".Decoder, m *map[K]V, name, field string, nullOK bool, pk func(string, string, string) (K, error), pv ", fg.parseFuncType("V"), ") error {")
	fg.P("if err := ", fg.fn("ExpectJSON"), "(d, ", fg.kind("BeginObject"), ", name, \"object\"); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("for d.PeekKind() != ", fg.kind("EndObject"), " {")
	fg.P("kt, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("k, err := pk(kt.String(), name, field)")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("if !nullOK && d.PeekKind() == ", fg.kind("Null"), " {")
	fg.P("return ", fg.jsonErrorf("name", "ErrNullElement", errNullElement))
	fg.P("}")
	fg.P("var v V")
	fg.P("if err := pv(d, &v, name); err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P(fg.fn("MapSet"), "(m, k, v)")
	fg.P("}")
	fg.P("_, err := d.ReadToken()")
	fg.P("return err")
	fg.P("}")
}

// genScalarParser emits a parser that reads one token, leaves *p unchanged
// on null, and otherwise runs body, which assigns *p from tok or returns an
// error.
func genScalarParser(fg *fileGen, name, typeParams, ptype string, body func()) {
	fg.P("func ", name, typeParams, "(d *", fg.jt(), ".Decoder, p *", ptype, ", name string) error {")
	fg.P("tok, err := d.ReadToken()")
	fg.P("if err != nil || tok.Kind() == ", fg.kind("Null"), " {")
	fg.P("return err")
	fg.P("}")
	body()
	fg.P("}")
}

// The integer and float parsers are generic over the field's Go type and
// derive its width with a shift or a division that is folded at compile
// time, so that they have the parseFuncType signature.
func genJSONInt(fg *fileGen, name string) {
	genScalarParser(fg, name, "[T ~int32 | ~int64]", "T", func() {
		fg.P("bits := 64")
		fg.P("if T(1)<<31 < 0 {")
		fg.P("bits = 32")
		fg.P("}")
		fg.P("v, err := ", fg.fn("ParseInt"), "(tok, bits, name)")
		fg.P("*p = T(v)")
		fg.P("return err")
	})
}

func genJSONUint(fg *fileGen, name string) {
	genScalarParser(fg, name, "[T ~uint32 | ~uint64]", "T", func() {
		fg.P("bits := 64")
		fg.P("if ^T(0)>>31 == 1 {")
		fg.P("bits = 32")
		fg.P("}")
		fg.P("v, err := ", fg.fn("ParseUint"), "(tok, bits, name)")
		fg.P("*p = T(v)")
		fg.P("return err")
	})
}

func genJSONFloat(fg *fileGen, name string) {
	genScalarParser(fg, name, "[T ~float32 | ~float64]", "T", func() {
		fg.P("bits := 64")
		fg.P("if float64(T(1)/T(3)) != 1.0/3 {")
		fg.P("bits = 32")
		fg.P("}")
		fg.P("v, err := ", fg.fn("ParseFloat"), "(tok, bits, name)")
		fg.P("*p = T(v)")
		fg.P("return err")
	})
}

func genJSONBool(fg *fileGen, name string) {
	genScalarParser(fg, name, "", "bool", func() {
		fg.P("switch tok.Kind() {")
		fg.P("case ", fg.kind("True"), ":")
		fg.P("*p = true")
		fg.P("case ", fg.kind("False"), ":")
		fg.P("*p = false")
		fg.P("default:")
		fg.P("return ", fg.jsonErrorf("name", "ErrInvalidBool", "invalid boolean ", "tok.String()"))
		fg.P("}")
		fg.P("return nil")
	})
}

func genJSONString(fg *fileGen, name string) {
	genScalarParser(fg, name, "", "string", func() {
		fg.P("if tok.Kind() != ", fg.kind("String"), " {")
		fg.P("return ", fg.jsonErrorf("name", "ErrInvalidString", "invalid string ", "tok.String()"))
		fg.P("}")
		fg.P("*p = tok.String()")
		fg.P("return nil")
	})
}

func genJSONBytes(fg *fileGen, name string) {
	b64 := fg.std("encoding/base64")
	genScalarParser(fg, name, "", "[]byte", func() {
		fg.P("s := tok.String()")
		fg.P("if tok.Kind() == ", fg.kind("String"), " {")
		fg.P("enc := ", b64, ".StdEncoding")
		fg.P("if ", fg.std("strings"), ".ContainsAny(s, \"-_\") {")
		fg.P("enc = ", b64, ".URLEncoding")
		fg.P("}")
		fg.P("if len(s)%", fg.c("Base64Quantum"), " != 0 {")
		fg.P("enc = enc.WithPadding(", b64, ".NoPadding)")
		fg.P("}")
		fg.P("if *p, err = enc.DecodeString(s); err == nil {")
		fg.P("return nil")
		fg.P("}")
		fg.P("}")
		fg.P("return ", fg.jsonErrorf("name", "ErrInvalidBytes", "invalid bytes ", "s"))
	})
}

func genJSONHex(fg *fileGen, name string) {
	genScalarParser(fg, name, "", "[]byte", func() {
		fg.P("s := tok.String()")
		fg.P("if tok.Kind() == ", fg.kind("String"), " {")
		fg.P("if *p, err = ", fg.std("encoding/hex"), ".DecodeString(s); err == nil {")
		fg.P("return nil")
		fg.P("}")
		fg.P("}")
		fg.P("return ", fg.jsonErrorf("name", "ErrInvalidHex", "invalid hex bytes ", "s"))
	})
}

// JSONEnum accepts a name or a number. null is a value only for
// google.protobuf.NullValue (nullOK); otherwise it leaves *p unchanged.
func genJSONEnum(fg *fileGen, name string) {
	fg.P("func ", name, "[E ~int32](d *", fg.jt(), ".Decoder, p *E, values map[string]int32, name, enum string, nullOK bool) error {")
	fg.P("tok, err := d.ReadToken()")
	fg.P("if err != nil {")
	fg.P("return err")
	fg.P("}")
	fg.P("switch tok.Kind() {")
	fg.P("case ", fg.kind("Null"), ":")
	fg.P("if nullOK {")
	fg.P("*p = 0")
	fg.P("}")
	fg.P("return nil")
	fg.P("case ", fg.kind("String"), ":")
	fg.P("s := tok.String()")
	fg.P("if n, ok := values[s]; ok {")
	fg.P("*p = E(n)")
	fg.P("return nil")
	fg.P("}")
	fg.P("return ", fg.jsonErrorf("name", "ErrInvalidEnum", "invalid value for enum ", "enum", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P("v, err := ", fg.fn("ParseInt"), "(tok, 32, name)")
	fg.P("*p = E(v)")
	fg.P("return err")
	fg.P("}")
}

// JSONMsg merges into the message at p, allocating it unless the value is
// null. The message type is a type parameter, so that the function has the
// parseFuncType signature; the message is already on the heap.
func genJSONMsg(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "[T any, PT interface {")
	fg.P("*T")
	fg.P("ProtoMergeJSONFrom(*", jt, ".Decoder) error")
	fg.P("}](d *", jt, ".Decoder, p **T, _ string) error {")
	genNullSkip(fg)
	fg.P("return PT(", fg.fn("Alloc"), "(p)).ProtoMergeJSONFrom(d)")
	fg.P("}")
}

// JSONMsgNull is JSONMsg for google.protobuf.Value, whose null is a value.
func genJSONMsgNull(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "[T any, PT interface {")
	fg.P("*T")
	fg.P("ProtoMergeJSONFrom(*", jt, ".Decoder) error")
	fg.P("}](d *", jt, ".Decoder, p **T, _ string) error {")
	fg.P("return PT(", fg.fn("Alloc"), "(p)).ProtoMergeJSONFrom(d)")
	fg.P("}")
}

func genParseInt(fg *fileGen, name string) {
	m := fg.std("math")
	fg.P("func ", name, "(tok ", fg.jt(), ".Token, bits int, name string) (int64, error) {")
	fg.P("s, err := ", fg.fn("JSONNumber"), "(tok, name)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("if v, err := ", fg.std("strconv"), ".ParseInt(s, 10, bits); err == nil {")
	fg.P("return v, nil")
	fg.P("}")
	fg.P("n := ", fg.fn("ExactInt"), "(s)")
	fg.P("if n == nil || !n.IsInt64() || (bits == 32 && (n.Int64() < ", m, ".MinInt32 || n.Int64() > ", m, ".MaxInt32)) {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidInteger", "invalid integer ", "s"))
	fg.P("}")
	fg.P("return n.Int64(), nil")
	fg.P("}")
}

func genParseUint(fg *fileGen, name string) {
	fg.P("func ", name, "(tok ", fg.jt(), ".Token, bits int, name string) (uint64, error) {")
	fg.P("s, err := ", fg.fn("JSONNumber"), "(tok, name)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("if v, err := ", fg.std("strconv"), ".ParseUint(s, 10, bits); err == nil {")
	fg.P("return v, nil")
	fg.P("}")
	fg.P("n := ", fg.fn("ExactInt"), "(s)")
	fg.P("if n == nil || !n.IsUint64() || (bits == 32 && n.Uint64() > ", fg.std("math"), ".MaxUint32) {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidInteger", "invalid integer ", "s"))
	fg.P("}")
	fg.P("return n.Uint64(), nil")
	fg.P("}")
}

func genExactInt(fg *fileGen, name string) {
	conv := fg.std("strconv")
	max := fg.c("MaxJSONExponent")
	fg.P("func ", name, "(s string) *", fg.std("math/big"), ".Int {")
	fg.P("if i := ", fg.std("strings"), ".IndexAny(s, \"eE\"); i >= 0 {")
	fg.P("if e, err := ", conv, ".Atoi(s[i+1:]); err != nil || e > ", max, " || e < -", max, " {")
	fg.P("return nil")
	fg.P("}")
	fg.P("}")
	fg.P("r, ok := new(", fg.std("math/big"), ".Rat).SetString(s)")
	fg.P("if !ok || !r.IsInt() {")
	fg.P("return nil")
	fg.P("}")
	fg.P("return r.Num()")
	fg.P("}")
}

func genParseFloat(fg *fileGen, name string) {
	m := fg.std("math")
	fg.P("func ", name, "(tok ", fg.jt(), ".Token, bits int, name string) (float64, error) {")
	fg.P("if tok.Kind() == ", fg.kind("String"), " {")
	fg.P("switch tok.String() {")
	fg.P("case \"NaN\":")
	fg.P("return ", m, ".NaN(), nil")
	fg.P("case \"Infinity\":")
	fg.P("return ", m, ".Inf(1), nil")
	fg.P("case \"-Infinity\":")
	fg.P("return ", m, ".Inf(-1), nil")
	fg.P("}")
	fg.P("}")
	fg.P("s, err := ", fg.fn("JSONNumber"), "(tok, name)")
	fg.P("if err != nil {")
	fg.P("return 0, err")
	fg.P("}")
	fg.P("v, err := ", fg.std("strconv"), ".ParseFloat(s, bits)")
	fg.P("if err != nil {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidNumber", "invalid number ", "s"))
	fg.P("}")
	fg.P("return v, nil")
	fg.P("}")
}

func genJSONNumber(fg *fileGen, name string) {
	jt := fg.jt()
	fg.P("func ", name, "(tok ", jt, ".Token, name string) (string, error) {")
	fg.P("s := tok.String()")
	fg.P("if k := tok.Kind(); k != ", fg.kind("Number"), " && (k != ", fg.kind("String"), " || s == \"\" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) || !", jt, ".Value(s).IsValid()) {")
	fg.P("return \"\", ", fg.jsonErrorf("name", "ErrInvalidNumber", "invalid number ", "s"))
	fg.P("}")
	fg.P("return s, nil")
	fg.P("}")
}

// The map key parsers share a signature, so that JSONMap takes any of them.
func genIntKey(fg *fileGen, name string) {
	fg.P("func ", name, "[K ~int32 | ~int64](s, name, field string) (K, error) {")
	fg.P("bits := 64")
	fg.P("if K(1)<<31 < 0 {")
	fg.P("bits = 32")
	fg.P("}")
	fg.P("k, err := ", fg.std("strconv"), ".ParseInt(s, 10, bits)")
	fg.P("if err != nil {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidKey", "invalid map key for field ", "field", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P("return K(k), nil")
	fg.P("}")
}

func genUintKey(fg *fileGen, name string) {
	fg.P("func ", name, "[K ~uint32 | ~uint64](s, name, field string) (K, error) {")
	fg.P("bits := 64")
	fg.P("if ^K(0)>>31 == 1 {")
	fg.P("bits = 32")
	fg.P("}")
	fg.P("k, err := ", fg.std("strconv"), ".ParseUint(s, 10, bits)")
	fg.P("if err != nil {")
	fg.P("return 0, ", fg.jsonErrorf("name", "ErrInvalidKey", "invalid map key for field ", "field", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
	fg.P("return K(k), nil")
	fg.P("}")
}

func genBoolKey(fg *fileGen, name string) {
	fg.P("func ", name, "(s, name, field string) (bool, error) {")
	fg.P("switch s {")
	fg.P("case \"true\":")
	fg.P("return true, nil")
	fg.P("case \"false\":")
	fg.P("return false, nil")
	fg.P("}")
	fg.P("return false, ", fg.jsonErrorf("name", "ErrInvalidKey", "invalid map key for field ", "field", `": "`, fg.std("strconv")+".Quote(s)"))
	fg.P("}")
}

func genStringKey(fg *fileGen, name string) {
	fg.P("func ", name, "(s, _, _ string) (string, error) { return s, nil }")
}

func genJSONError(fg *fileGen, name string) {
	fg.P("func ", name, "(name, msg string) error { return ", fg.std("errors"), ".New(\"proto: \" + name + \": \" + msg) }")
}
