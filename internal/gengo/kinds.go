// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package gengo

import (
	"fmt"
	"math"
	"strconv"

	"github.com/canonical/cotorp/internal/desc"
)

// Wire types.
const (
	wireVarint     = 0
	wireFixed64    = 1
	wireBytes      = 2
	wireStartGroup = 3
	wireEndGroup   = 4
	wireFixed32    = 5
)

// wireTypeConst names the constant for a wire type in generated code.
var wireTypeConst = [...]string{
	wireVarint:     "WireVarint",
	wireFixed64:    "WireFixed64",
	wireBytes:      "WireBytes",
	wireStartGroup: "WireStartGroup",
	wireEndGroup:   "WireEndGroup",
	wireFixed32:    "WireFixed32",
}

// Error messages returned by generated code.
const (
	errParseMsg     = "proto: cannot parse invalid wire-format data"
	errDepthMsg     = "proto: exceeded maximum recursion depth"
	errSizeMsg      = "proto: message size changed during marshal"
	errRequiredFmt  = "proto: required field %s not set"
	errInvalidUTF8F = "proto: field %s contains invalid UTF-8"
)

// wire returns the constant naming wire type wt.
func (fg *fileGen) wire(wt int) string {
	return fg.c(wireTypeConst[wt])
}

// fieldNum returns an expression for f's field number: a map entry constant
// for map keys and values, otherwise the number itself.
func (fg *fileGen) fieldNum(f *desc.Field) string {
	if f.Parent != nil && f.Parent.IsMapEntry {
		if f.Number == 1 {
			return fg.c("MapKeyField")
		}
		return fg.c("MapValueField")
	}
	return strconv.Itoa(int(f.Number))
}

// tagExpr returns a constant expression for the tag of field number
// expression num with wire type wt.
func (fg *fileGen) tagExpr(num string, wt int) string {
	return num + "<<" + fg.c("TagTypeBits") + " | " + fg.wire(wt)
}

func wireType(f *desc.Field) int {
	switch f.Kind {
	case desc.KindDouble, desc.KindFixed64, desc.KindSfixed64:
		return wireFixed64
	case desc.KindFloat, desc.KindFixed32, desc.KindSfixed32:
		return wireFixed32
	case desc.KindString, desc.KindBytes:
		return wireBytes
	case desc.KindMessage:
		if f.Delimited {
			return wireStartGroup
		}
		return wireBytes
	}
	return wireVarint
}

func tagBytes(num int32, wt int) []byte {
	v := uint64(num)<<3 | uint64(wt)
	var b []byte
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func tagSize(num int32, wt int) int { return len(tagBytes(num, wt)) }

// fixedSize returns the encoded size of fixed-width kinds, or 0.
func fixedSize(k desc.Kind) int {
	switch k {
	case desc.KindDouble, desc.KindFixed64, desc.KindSfixed64:
		return 8
	case desc.KindFloat, desc.KindFixed32, desc.KindSfixed32:
		return 4
	case desc.KindBool:
		return 1
	}
	return 0
}

// Standard library packages used by generated code, by import path.
var stdPackages = map[string]string{
	"bytes":                  "bytes",
	"cmp":                    "cmp",
	"encoding/base64":        "base64",
	"encoding/binary":        "binary",
	"encoding/hex":           "hex",
	"encoding/json/jsontext": "jsontext",
	"encoding/json/v2":       "json",
	"io":                     "io",
	"math/big":               "big",
	"strings":                "strings",
	"time":                   "time",
	"errors":                 "errors",
	"math":                   "math",
	"math/bits":              "bits",
	"slices":                 "slices",
	"strconv":                "strconv",
	"unicode/utf8":           "utf8",
}

// std records a use of a standard library package and returns its name.
func (fg *fileGen) std(path string) string {
	fg.stdImports[path] = true
	return stdPackages[path]
}

// errExpr returns an expression constructing an error with message msg.
// If msg is used more than once in the file it is declared as a constant
// named by hint (see errString).
func (fg *fileGen) errExpr(hint, msg string) string {
	return fg.std("errors") + ".New(" + fg.errString(hint, msg) + ")"
}

// scalarGoType returns the Go type for a field kind.
func (fg *fileGen) scalarGoType(f *desc.Field) string {
	switch f.Kind {
	case desc.KindDouble:
		return "float64"
	case desc.KindFloat:
		return "float32"
	case desc.KindInt64, desc.KindSint64, desc.KindSfixed64:
		return "int64"
	case desc.KindUint64, desc.KindFixed64:
		return "uint64"
	case desc.KindInt32, desc.KindSint32, desc.KindSfixed32:
		return "int32"
	case desc.KindUint32, desc.KindFixed32:
		return "uint32"
	case desc.KindBool:
		return "bool"
	case desc.KindString:
		return "string"
	case desc.KindBytes:
		return "[]byte"
	case desc.KindEnum:
		return fg.enumType(f.EnumType)
	case desc.KindMessage:
		return "*" + fg.msgType(f.MessageType)
	}
	panic("unknown kind")
}

// varintExpr converts v to the uint64 written on the wire for varint kinds
// other than bool.
func varintExpr(k desc.Kind, v string) string {
	switch k {
	case desc.KindInt32, desc.KindEnum:
		return "uint64(int64(" + v + "))"
	case desc.KindInt64, desc.KindUint32:
		return "uint64(" + v + ")"
	case desc.KindUint64:
		return v
	case desc.KindSint32:
		return "uint64(uint32((" + v + ")<<1)^uint32((" + v + ")>>31))"
	case desc.KindSint64:
		return "(uint64((" + v + ")<<1)^uint64((" + v + ")>>63))"
	}
	panic("not a varint kind: " + k.String())
}

// sizeVarint returns an expression for the encoded size of uint64 value e,
// which must be a primary expression (an operand, call or conversion).
func (fg *fileGen) sizeVarint(e string) string {
	pb := fg.c("VarintPayloadBits")
	return "(" + fg.std("math/bits") + ".Len64(" + e + "|1)+" + pb + "-1)/" + pb
}

// fixedSizeExpr returns an expression for fixedSize(k), which must be
// nonzero.
func (fg *fileGen) fixedSizeExpr(k desc.Kind) string {
	switch fixedSize(k) {
	case 8:
		return fg.c("Fixed64Size")
	case 4:
		return fg.c("Fixed32Size")
	}
	return "1"
}

// sizeExpr returns the encoded size of scalar value v, excluding the tag.
func (fg *fileGen) sizeExpr(k desc.Kind, v string) string {
	if fixedSize(k) > 0 {
		return fg.fixedSizeExpr(k)
	}
	switch k {
	case desc.KindString, desc.KindBytes:
		return "len(" + v + ") + " + fg.sizeVarint("uint64(len("+v+"))")
	}
	return fg.sizeVarint(varintExpr(k, v))
}

// encVarint emits code writing uint64 expression e backwards at b[:i].
func (fg *fileGen) encVarint(e string) {
	fg.P("i = ", fg.fn("PutVarint"), "(b, i, ", e, ")")
}

// encode emits code writing scalar v (without tag) backwards at b[:i].
func (fg *fileGen) encode(k desc.Kind, v string) {
	le := func() string { return fg.std("encoding/binary") + ".LittleEndian" }
	switch k {
	case desc.KindDouble:
		fg.P("i -= ", fg.c("Fixed64Size"))
		fg.P(le(), ".PutUint64(b[i:], ", fg.std("math"), ".Float64bits(", v, "))")
	case desc.KindFixed64, desc.KindSfixed64:
		fg.P("i -= ", fg.c("Fixed64Size"))
		fg.P(le(), ".PutUint64(b[i:], uint64(", v, "))")
	case desc.KindFloat:
		fg.P("i -= ", fg.c("Fixed32Size"))
		fg.P(le(), ".PutUint32(b[i:], ", fg.std("math"), ".Float32bits(", v, "))")
	case desc.KindFixed32, desc.KindSfixed32:
		fg.P("i -= ", fg.c("Fixed32Size"))
		fg.P(le(), ".PutUint32(b[i:], uint32(", v, "))")
	case desc.KindBool:
		fg.P("i--")
		fg.P("if ", v, " {")
		fg.P("b[i] = 1")
		fg.P("} else {")
		fg.P("b[i] = 0")
		fg.P("}")
	case desc.KindString, desc.KindBytes:
		fg.P("i -= len(", v, ")")
		fg.P("copy(b[i:], ", v, ")")
		fg.encVarint("uint64(len(" + v + "))")
	default:
		fg.encVarint(varintExpr(k, v))
	}
}

// nonZeroExpr returns a condition that is true when implicit-presence value
// v must be serialized.
func (fg *fileGen) nonZeroExpr(k desc.Kind, v string) string {
	switch k {
	case desc.KindDouble:
		return fg.std("math") + ".Float64bits(" + v + ") != 0"
	case desc.KindFloat:
		return fg.std("math") + ".Float32bits(" + v + ") != 0"
	case desc.KindBool:
		return v
	case desc.KindString, desc.KindBytes:
		return "len(" + v + ") > 0"
	}
	return v + " != 0"
}

// decBytes emits code decoding a length-delimited value from buf into a new
// variable dst, setting n to the consumed length. It must be used inside
// ProtoMergeDepth, which defines the errParse label.
func (fg *fileGen) decBytes(dst, buf string) {
	fg.P(dst, ", n := ", fg.fn("ReadBytes"), "(", buf, ")")
	fg.P("if n < 0 {")
	fg.P("goto errParse")
	fg.P("}")
}

// decodeScalar emits code decoding one value of f's kind from buf into the
// raw variable x (consumed length in n) and returns an expression converting
// x to the Go type.
func (fg *fileGen) decodeScalar(f *desc.Field, buf string) string {
	bin := fg.std("encoding/binary")
	switch wireType(f) {
	case wireVarint:
		fg.P("x, n := ", bin, ".Uvarint(", buf, ")")
		fg.P("if n <= 0 {")
		fg.P("goto errParse")
		fg.P("}")
	case wireFixed32:
		fs := fg.c("Fixed32Size")
		fg.P("if len(", buf, ") < ", fs, " {")
		fg.P("goto errParse")
		fg.P("}")
		fg.P("x, n := ", bin, ".LittleEndian.Uint32(", buf, "), ", fs)
	case wireFixed64:
		fs := fg.c("Fixed64Size")
		fg.P("if len(", buf, ") < ", fs, " {")
		fg.P("goto errParse")
		fg.P("}")
		fg.P("x, n := ", bin, ".LittleEndian.Uint64(", buf, "), ", fs)
	case wireBytes:
		fg.decBytes("x", buf)
	}
	switch f.Kind {
	case desc.KindInt32, desc.KindSfixed32:
		return "int32(x)"
	case desc.KindEnum:
		return fg.enumType(f.EnumType) + "(int32(x))"
	case desc.KindUint32:
		return "uint32(x)"
	case desc.KindInt64, desc.KindSfixed64:
		return "int64(x)"
	case desc.KindUint64, desc.KindFixed64, desc.KindFixed32:
		return "x"
	case desc.KindSint32:
		return "int32(uint32(x)>>1) ^ -int32(x&1)"
	case desc.KindSint64:
		return "int64(x>>1) ^ -int64(x&1)"
	case desc.KindBool:
		return "x != 0"
	case desc.KindFloat:
		return fg.std("math") + ".Float32frombits(x)"
	case desc.KindDouble:
		return fg.std("math") + ".Float64frombits(x)"
	case desc.KindString:
		if f.ValidateUTF8 {
			fg.P("if !", fg.std("unicode/utf8"), ".Valid(x) {")
			fg.P("return ", fg.errExpr(fg.owner(f.FullName)+"ErrUTF8", fmt.Sprintf(errInvalidUTF8F, f.FullName)))
			fg.P("}")
		}
		return "string(x)"
	case desc.KindBytes:
		return "append([]byte{}, x...)"
	}
	panic("decodeScalar: unexpected kind " + f.Kind.String())
}

// emitCheckTag emits code that validates a tag decoded into t and n.
func (fg *fileGen) emitCheckTag(t, n string) {
	tb := fg.c("TagTypeBits")
	fg.P("if ", n, " <= 0 || ", t, ">>", tb, " == 0 || ", t, ">>", tb, " > ", fg.c("MaxFieldNumber"), " {")
	fg.P("goto errParse")
	fg.P("}")
}

// zeroExpr is the zero value literal for a field's singular Go type.
func (fg *fileGen) zeroExpr(f *desc.Field) string {
	switch f.Kind {
	case desc.KindBool:
		return "false"
	case desc.KindString:
		return `""`
	case desc.KindBytes, desc.KindMessage:
		return "nil"
	case desc.KindEnum:
		if len(f.EnumType.Values) > 0 {
			return fg.enumValue(f.EnumType.Values[0])
		}
		return "0"
	}
	return "0"
}

// floatLiteral formats a finite float as a Go constant expression.
func floatLiteral(v float64, bits int) (string, bool) {
	if math.IsInf(v, 0) || math.IsNaN(v) || (v == 0 && math.Signbit(v)) {
		return "", false
	}
	return strconv.FormatFloat(v, 'g', -1, bits), true
}

func (fg *fileGen) floatSpecialExpr(v float64) string {
	m := fg.std("math")
	switch {
	case math.IsInf(v, 1):
		return m + ".Inf(1)"
	case math.IsInf(v, -1):
		return m + ".Inf(-1)"
	case math.IsNaN(v):
		return m + ".NaN()"
	default:
		return m + ".Copysign(0, -1)"
	}
}
