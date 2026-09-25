// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"math"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	commonpb "github.com/canonical/cotorp/internal/testprotos/common"
	editionspb "github.com/canonical/cotorp/internal/testprotos/editions"
	proto2pb "github.com/canonical/cotorp/internal/testprotos/proto2"
	proto3pb "github.com/canonical/cotorp/internal/testprotos/proto3"
)

type message interface {
	MarshalBinary() ([]byte, error)
	UnmarshalBinary([]byte) error
	ProtoSize() int
}

func needProtoc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found in PATH")
	}
}

func runProtoc(t *testing.T, stdin []byte, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("protoc", append([]string{"-Iproto"}, args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("protoc %v: %v\n%s", args, err, stderr.String())
	}
	return stdout.Bytes()
}

func protocEncode(t *testing.T, file, typ, text string) []byte {
	t.Helper()
	return runProtoc(t, []byte(text), "--encode="+typ, file)
}

func protocDecode(t *testing.T, file, typ string, data []byte) string {
	t.Helper()
	return string(runProtoc(t, data, "--decode="+typ, file))
}

func protocDecodeRaw(t *testing.T, data []byte) string {
	t.Helper()
	return string(runProtoc(t, data, "--decode_raw"))
}

type compatCase struct {
	name string
	file string
	typ  string
	msg  message
	text string
	// unordered is set when the encoding order is not deterministic in
	// protoc (maps), so bytes are compared through protoc's text output.
	unordered bool
}

func newOf(m message) message {
	return reflect.New(reflect.TypeOf(m).Elem()).Interface().(message)
}

func runCompat(t *testing.T, cases []compatCase) {
	needProtoc(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := protocEncode(t, tc.file, tc.typ, tc.text)
			got, err := tc.msg.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if len(got) != tc.msg.ProtoSize() {
				t.Errorf("ProtoSize() = %d, encoded %d bytes", tc.msg.ProtoSize(), len(got))
			}
			if !tc.unordered && !bytes.Equal(got, want) {
				t.Errorf("encoding mismatch\n got: % x\nwant: % x\n got text:\n%s\nwant text:\n%s",
					got, want, protocDecodeRaw(t, got), protocDecodeRaw(t, want))
			}
			if gt, wt := protocDecode(t, tc.file, tc.typ, got), protocDecode(t, tc.file, tc.typ, want); gt != wt {
				t.Errorf("decoded text mismatch\n got:\n%s\nwant:\n%s", gt, wt)
			}

			// Decode protoc's encoding and compare with the Go value.
			dec := newOf(tc.msg)
			if err := dec.UnmarshalBinary(want); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			if !reflect.DeepEqual(dec, tc.msg) {
				t.Errorf("decoded value mismatch\n got: %+v\nwant: %+v", dec, tc.msg)
			}
			re, err := dec.MarshalBinary()
			if err != nil {
				t.Fatalf("re-MarshalBinary: %v", err)
			}
			if !bytes.Equal(re, got) {
				t.Errorf("re-encoding mismatch\n got: % x\nwant: % x", re, got)
			}
		})
	}
}

func TestProto3Compat(t *testing.T) {
	const file = "proto3/scalars.proto"
	const pkg = "cotorp.test.proto3."
	runCompat(t, []compatCase{
		{
			name: "scalars-empty",
			file: file, typ: pkg + "Scalars",
			msg:  &proto3pb.Scalars{},
			text: ``,
		},
		{
			name: "scalars-full",
			file: file, typ: pkg + "Scalars",
			msg: &proto3pb.Scalars{
				FDouble:   1.5,
				FFloat:    -2.25,
				FInt64:    -9000000000,
				FUint64:   math.MaxUint64,
				FInt32:    -1,
				FFixed64:  123,
				FFixed32:  math.MaxUint32,
				FBool:     true,
				FString:   "héllo",
				FBytes:    []byte{0, 0xff},
				FUint32:   7,
				FSfixed32: -5,
				FSfixed64: -6,
				FSint32:   math.MinInt32,
				FSint64:   math.MinInt64,
				FEnum:     commonpb.Color_COLOR_BLUE,
				FNested:   &proto3pb.Scalars_Nested{A: 1, Recursive: &proto3pb.Scalars_Nested{A: 2}},
				FShared:   &commonpb.Shared{Label: "x", Value: 3},
			},
			text: `f_double: 1.5 f_float: -2.25 f_int64: -9000000000
				f_uint64: 18446744073709551615 f_int32: -1 f_fixed64: 123
				f_fixed32: 4294967295 f_bool: true f_string: "héllo"
				f_bytes: "\x00\xff" f_uint32: 7 f_sfixed32: -5 f_sfixed64: -6
				f_sint32: -2147483648 f_sint64: -9223372036854775808
				f_enum: COLOR_BLUE f_nested { a: 1 recursive { a: 2 } }
				f_shared { label: "x" value: 3 }`,
		},
		{
			name: "scalars-negative-zero",
			file: file, typ: pkg + "Scalars",
			msg:  &proto3pb.Scalars{FDouble: math.Copysign(0, -1), FFloat: float32(math.Copysign(0, -1))},
			text: `f_double: -0 f_float: -0`,
		},
		{
			name: "scalars-empty-submessage",
			file: file, typ: pkg + "Scalars",
			msg:  &proto3pb.Scalars{FNested: &proto3pb.Scalars_Nested{}},
			text: `f_nested {}`,
		},
		{
			name: "optionals-zero",
			file: file, typ: pkg + "Optionals",
			msg: &proto3pb.Optionals{
				ODouble: new(0.0), OFloat: new(float32(0)), OInt64: new(int64(0)),
				OUint64: new(uint64(0)), OInt32: new(int32(0)), OFixed64: new(uint64(0)),
				OFixed32: new(uint32(0)), OBool: new(false), OString: new(""),
				OBytes: []byte{}, OUint32: new(uint32(0)), OSfixed32: new(int32(0)),
				OSfixed64: new(int64(0)), OSint32: new(int32(0)), OSint64: new(int64(0)),
				OEnum: commonpb.Color_COLOR_UNSPECIFIED.Enum(),
			},
			text: `o_double: 0 o_float: 0 o_int64: 0 o_uint64: 0 o_int32: 0
				o_fixed64: 0 o_fixed32: 0 o_bool: false o_string: "" o_bytes: ""
				o_uint32: 0 o_sfixed32: 0 o_sfixed64: 0 o_sint32: 0 o_sint64: 0
				o_enum: COLOR_UNSPECIFIED`,
		},
		{
			name: "optionals-values",
			file: file, typ: pkg + "Optionals",
			msg: &proto3pb.Optionals{
				OInt32: new(int32(-3)), OString: new("s"), OBytes: []byte("b"),
				OEnum: commonpb.Color_COLOR_RED.Enum(), OSint64: new(int64(-99)),
			},
			text: `o_int32: -3 o_string: "s" o_bytes: "b" o_sint64: -99 o_enum: COLOR_RED`,
		},
		{
			name: "repeateds",
			file: file, typ: pkg + "Repeateds",
			msg: &proto3pb.Repeateds{
				RDouble:   []float64{1, -2.5, math.Inf(1)},
				RFloat:    []float32{0, 3.25},
				RInt64:    []int64{-1, 0, math.MaxInt64},
				RUint64:   []uint64{0, math.MaxUint64},
				RInt32:    []int32{-1, 1, 300},
				RFixed64:  []uint64{1},
				RFixed32:  []uint32{2, 3},
				RBool:     []bool{true, false, true},
				RString:   []string{"", "a", "bc"},
				RBytes:    [][]byte{{}, {1}},
				RUint32:   []uint32{math.MaxUint32},
				RSfixed32: []int32{-1},
				RSfixed64: []int64{-2},
				RSint32:   []int32{-1, 1, math.MinInt32},
				RSint64:   []int64{math.MinInt64, math.MaxInt64},
				REnum:     []commonpb.Color{commonpb.Color_COLOR_GREEN, commonpb.Color_COLOR_UNSPECIFIED},
				RNested:   []*proto3pb.Scalars_Nested{{A: 1}, {}},
				RUnpacked: []int32{5, -5},
			},
			text: `r_double: [1, -2.5, inf] r_float: [0, 3.25]
				r_int64: [-1, 0, 9223372036854775807] r_uint64: [0, 18446744073709551615]
				r_int32: [-1, 1, 300] r_fixed64: 1 r_fixed32: [2, 3]
				r_bool: [true, false, true] r_string: ["", "a", "bc"] r_bytes: ["", "\x01"]
				r_uint32: 4294967295 r_sfixed32: -1 r_sfixed64: -2
				r_sint32: [-1, 1, -2147483648]
				r_sint64: [-9223372036854775808, 9223372036854775807]
				r_enum: [COLOR_GREEN, COLOR_UNSPECIFIED]
				r_nested { a: 1 } r_nested {}
				r_unpacked: [5, -5]`,
		},
		{
			name: "maps",
			file: file, typ: pkg + "Maps",
			msg: &proto3pb.Maps{
				MStringString: map[string]string{"a": "1", "b": "", "": "empty"},
				MInt32Int64:   map[int32]int64{-1: -1, 0: 0, 5: 1 << 40},
				MBoolBytes:    map[bool][]byte{true: {1, 2}, false: {}},
				MSint64Nested: map[int64]*proto3pb.Scalars_Nested{-7: {A: 7}, 8: {}},
				MFixed32Enum:  map[uint32]commonpb.Color{1: commonpb.Color_COLOR_RED, 2: commonpb.Color_COLOR_UNSPECIFIED},
				MUint64Double: map[uint64]float64{math.MaxUint64: 0.5},
				MStringShared: map[string]*commonpb.Shared{"k": {Label: "l", Value: -1}},
			},
			text: `m_string_string { key: "a" value: "1" }
				m_string_string { key: "b" value: "" }
				m_string_string { key: "" value: "empty" }
				m_int32_int64 { key: -1 value: -1 }
				m_int32_int64 { key: 0 value: 0 }
				m_int32_int64 { key: 5 value: 1099511627776 }
				m_bool_bytes { key: true value: "\x01\x02" }
				m_bool_bytes { key: false value: "" }
				m_sint64_nested { key: -7 value { a: 7 } }
				m_sint64_nested { key: 8 value {} }
				m_fixed32_enum { key: 1 value: COLOR_RED }
				m_fixed32_enum { key: 2 value: COLOR_UNSPECIFIED }
				m_uint64_double { key: 18446744073709551615 value: 0.5 }
				m_string_shared { key: "k" value { label: "l" value: -1 } }`,
			unordered: true,
		},
		{
			name: "oneof-int32-zero",
			file: file, typ: pkg + "Oneofs",
			msg:  &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CInt32{CInt32: 0}},
			text: `c_int32: 0`,
		},
		{
			name: "oneof-string",
			file: file, typ: pkg + "Oneofs",
			msg:  &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CString{CString: "x"}, After: 1, Other: &proto3pb.Oneofs_OFixed32{OFixed32: 9}},
			text: `c_string: "x" after: 1 o_fixed32: 9`,
		},
		{
			name: "oneof-bytes-empty",
			file: file, typ: pkg + "Oneofs",
			msg:  &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CBytes{CBytes: []byte{}}},
			text: `c_bytes: ""`,
		},
		{
			name: "oneof-message",
			file: file, typ: pkg + "Oneofs",
			msg:  &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CNested{CNested: &proto3pb.Scalars_Nested{A: 4}}, Other: &proto3pb.Oneofs_OSint64{OSint64: -1}},
			text: `c_nested { a: 4 } o_sint64: -1`,
		},
		{
			name: "oneof-enum-double",
			file: file, typ: pkg + "Oneofs",
			msg:  &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CEnum{CEnum: commonpb.Color_COLOR_UNSPECIFIED}},
			text: `c_enum: COLOR_UNSPECIFIED`,
		},
		{
			name: "names",
			file: file, typ: pkg + "Names",
			msg:  &proto3pb.Names{Reset_: 1, ProtoSize_: 2, GetFoo: 3, Foo_: 4, MarshalBinary_: "m"},
			text: `reset: 1 proto_size: 2 get_foo: 3 foo: 4 marshal_binary: "m"`,
		},
	})
}

func TestProto2Compat(t *testing.T) {
	const file = "proto2/proto2.proto"
	const pkg = "cotorp.test.proto2."
	runCompat(t, []compatCase{
		{
			name: "defaults-unset",
			file: file, typ: pkg + "Defaults",
			msg:  &proto2pb.Defaults{},
			text: ``,
		},
		{
			name: "defaults-set",
			file: file, typ: pkg + "Defaults",
			msg: &proto2pb.Defaults{
				DInt32: new(int32(0)), DString: new(""), DBytes: []byte{},
				DEnum: proto2pb.Closed_CLOSED_ONE.Enum(), DBool: new(false),
			},
			text: `d_int32: 0 d_bool: false d_string: "" d_bytes: "" d_enum: CLOSED_ONE`,
		},
		{
			name: "required",
			file: file, typ: pkg + "Required",
			msg: &proto2pb.Required{
				ReqInt32: new(int32(1)), ReqString: new("r"),
				Inner:  &proto2pb.Required_Inner{Flag: new(true)},
				Inners: []*proto2pb.Required_Inner{{Flag: new(false)}},
				O:      &proto2pb.Required_InnerOneof{InnerOneof: &proto2pb.Required_Inner{Flag: new(true)}},
			},
			text: `req_int32: 1 req_string: "r" inner { flag: true } inners { flag: false }
				inner_oneof { flag: true }`,
		},
		{
			name: "groups",
			file: file, typ: pkg + "Groups",
			msg: &proto2pb.Groups{
				Optgroup: &proto2pb.Groups_OptGroup{A: new(int32(1)), B: new("x")},
				Repgroup: []*proto2pb.Groups_RepGroup{
					{C: new(int32(2)), Deep: &proto2pb.Groups_RepGroup_Deep{D: new(int64(-3))}},
					{},
				},
				After: new(int32(9)),
			},
			text: `OptGroup { a: 1 b: "x" } RepGroup { c: 2 Deep { d: -3 } } RepGroup {} after: 9`,
		},
		{
			name: "closed-enums",
			file: file, typ: pkg + "ClosedEnums",
			msg: &proto2pb.ClosedEnums{
				Single: proto2pb.Closed_CLOSED_TEN.Enum(),
				Rep:    []proto2pb.Closed{proto2pb.Closed_CLOSED_ONE, proto2pb.Closed_CLOSED_TWO},
				Packed: []proto2pb.Closed{proto2pb.Closed_CLOSED_TWO},
				ByKey:  map[int32]proto2pb.ClosedZero{3: proto2pb.ClosedZero_CLOSED_ZERO_ONE},
				O:      &proto2pb.ClosedEnums_InOneof{InOneof: proto2pb.Closed_CLOSED_ONE},
			},
			text: `single: CLOSED_TEN rep: [CLOSED_ONE, CLOSED_TWO] packed: CLOSED_TWO
				by_key { key: 3 value: CLOSED_ZERO_ONE } in_oneof: CLOSED_ONE`,
		},
		{
			name: "packed",
			file: file, typ: pkg + "Packed",
			msg: &proto2pb.Packed{
				PInt32:   []int32{1, -1},
				PSint64:  []int64{-2, 2},
				PFixed32: []uint32{3},
				PDouble:  []float64{0.5},
				PBool:    []bool{false, true},
				UInt32:   []int32{7, 8},
			},
			text: `p_int32: [1, -1] p_sint64: [-2, 2] p_fixed32: 3 p_double: 0.5
				p_bool: [false, true] u_int32: [7, 8]`,
		},
		{
			name: "strings-invalid-utf8-allowed",
			file: file, typ: pkg + "Proto2Strings",
			msg:  &proto2pb.Proto2Strings{S: new("\xff"), Rs: []string{"\xfe"}, Shared: &commonpb.Shared{Label: "ok"}},
			text: `s: "\xff" rs: "\xfe" shared { label: "ok" }`,
		},
	})
}

func TestEditionsCompat(t *testing.T) {
	const file = "editions/editions.proto"
	const pkg = "cotorp.test.editions."
	runCompat(t, []compatCase{
		{
			name: "required-only",
			file: file, typ: pkg + "Features",
			msg:  &editionspb.Features{RequiredInt: new(int32(0))},
			text: `required_int: 0`,
		},
		{
			name: "features",
			file: file, typ: pkg + "Features",
			msg: &editionspb.Features{
				ExplicitInt:    new(int32(0)),
				ImplicitInt:    5,
				RequiredInt:    new(int32(6)),
				PackedInts:     []int32{1, 2},
				ExpandedInts:   []int32{3, 4},
				Verified:       new("v"),
				Unverified:     new("\xff"),
				Delimited:      &editionspb.Features_Child{X: new(int32(1))},
				DelimitedList:  []*editionspb.Features_Child{{X: new(int32(2))}, {}},
				LengthPrefixed: &editionspb.Features_Child{X: new(int32(3))},
				OpenEnum:       editionspb.OpenEnum_OPEN_ZERO.Enum(),
				ClosedEnum:     editionspb.ClosedEnum_CLOSED_TWO.Enum(),
				WithDefault:    new("x"),
				VerifiedMap:    map[string]string{"k": "v"},
				UnverifiedMap:  map[string]string{"\xff": "\xfe"},
			},
			text: `explicit_int: 0 implicit_int: 5 required_int: 6 packed_ints: [1, 2]
				expanded_ints: [3, 4] verified: "v" unverified: "\xff"
				delimited { x: 1 } delimited_list { x: 2 } delimited_list {}
				length_prefixed { x: 3 } open_enum: OPEN_ZERO closed_enum: CLOSED_TWO
				with_default: "x" verified_map { key: "k" value: "v" }
				unverified_map { key: "\xff" value: "\xfe" }`,
		},
		{
			name: "implicit",
			file: file, typ: pkg + "ImplicitMessage",
			msg:  &editionspb.ImplicitMessage{A: 1, B: "b", C: &editionspb.Features_Child{}},
			text: `a: 1 b: "b" c {}`,
		},
	})
}

func TestUnknownFieldsPreserved(t *testing.T) {
	// Field 99 is unknown; field 1 has the wrong wire type for f_double.
	in := []byte{0x98, 0x06, 0x05, 0x08, 0x07}
	var m proto3pb.Scalars
	if err := m.UnmarshalBinary(in); err != nil {
		t.Fatal(err)
	}
	if m.FDouble != 0 {
		t.Errorf("FDouble = %v, want 0", m.FDouble)
	}
	if !bytes.Equal(m.ProtoUnknownFields(), in) {
		t.Errorf("unknown fields = % x, want % x", m.ProtoUnknownFields(), in)
	}
	out, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("round trip = % x, want % x", out, in)
	}
}

func TestExtensionsPreserved(t *testing.T) {
	needProtoc(t)
	const file = "proto2/proto2.proto"
	want := protocEncode(t, file, "cotorp.test.proto2.Extendable",
		`known: 1 [cotorp.test.proto2.ext_int32]: 5
		 [cotorp.test.proto2.extgroup] { g: 2 }
		 [cotorp.test.proto2.Scope.ext_string]: "s"`)
	var m proto2pb.Extendable
	if err := m.UnmarshalBinary(want); err != nil {
		t.Fatal(err)
	}
	if m.GetKnown() != 1 {
		t.Errorf("Known = %d", m.GetKnown())
	}
	got, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("round trip = % x, want % x", got, want)
	}
}

func TestClosedEnumUnknownValues(t *testing.T) {
	needProtoc(t)
	// single=5 (unknown), rep=[1, 99], packed=[2, 77], by_key{3: 42}, in_oneof=88.
	in := []byte{
		0x08, 0x05,
		0x10, 0x01, 0x10, 0x63,
		0x1a, 0x02, 0x02, 0x4d,
		0x22, 0x04, 0x08, 0x03, 0x10, 0x2a,
		0x28, 0x58,
	}
	var m proto2pb.ClosedEnums
	if err := m.UnmarshalBinary(in); err != nil {
		t.Fatal(err)
	}
	want := &proto2pb.ClosedEnums{
		Rep:    []proto2pb.Closed{proto2pb.Closed_CLOSED_ONE},
		Packed: []proto2pb.Closed{proto2pb.Closed_CLOSED_TWO},
	}
	if m.Single != nil || m.O != nil || len(m.ByKey) != 0 ||
		!reflect.DeepEqual(m.Rep, want.Rep) || !reflect.DeepEqual(m.Packed, want.Packed) {
		t.Errorf("decoded %+v", &m)
	}
	out, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	// protoc must see the same message: known values as fields, unknown
	// values as unknown fields.
	gotText := protocDecode(t, "proto2/proto2.proto", "cotorp.test.proto2.ClosedEnums", out)
	wantText := "rep: CLOSED_ONE\npacked: CLOSED_TWO\n1: 5\n2: 99\n3: 77\n4 {\n  1: 3\n  2: 42\n}\n5: 88\n"
	if gotText != wantText {
		t.Errorf("protoc decode:\n%s\nwant:\n%s", gotText, wantText)
	}
}

func TestDefaultsGetters(t *testing.T) {
	var m *proto2pb.Defaults // nil receiver must work
	if got := m.GetDInt32(); got != -42 {
		t.Errorf("DInt32 = %d", got)
	}
	if got := m.GetDInt64(); got != math.MinInt64 {
		t.Errorf("DInt64 = %d", got)
	}
	if got := m.GetDUint64(); got != math.MaxUint64 {
		t.Errorf("DUint64 = %d", got)
	}
	if got := m.GetDFixed32(); got != 0777 {
		t.Errorf("DFixed32 = %d", got)
	}
	if got := m.GetDFloat(); got != 1500 {
		t.Errorf("DFloat = %v", got)
	}
	if got := m.GetDDouble(); !math.IsInf(got, -1) {
		t.Errorf("DDouble = %v", got)
	}
	if got := m.GetDNan(); !math.IsNaN(got) {
		t.Errorf("DNan = %v", got)
	}
	if got := m.GetDBool(); !got {
		t.Errorf("DBool = %v", got)
	}
	if got := m.GetDString(); got != "h\"i\x01é" {
		t.Errorf("DString = %q", got)
	}
	if got := m.GetDBytes(); !bytes.Equal(got, []byte{0, 1, 0xff}) {
		t.Errorf("DBytes = %q", got)
	}
	if got := m.GetDEnum(); got != proto2pb.Closed_CLOSED_TEN {
		t.Errorf("DEnum = %v", got)
	}
	if got := m.GetDEnumNodefault(); got != proto2pb.Closed_CLOSED_ONE {
		t.Errorf("DEnumNodefault = %v", got)
	}
	if got := m.GetDFloatNegZero(); got != 0 || !math.Signbit(float64(got)) {
		t.Errorf("DFloatNegZero = %v", got)
	}
	if got := m.GetDDoubleInt(); got != 10 {
		t.Errorf("DDoubleInt = %v", got)
	}
	set := &proto2pb.Defaults{DInt32: new(int32(1))}
	if got := set.GetDInt32(); got != 1 {
		t.Errorf("set DInt32 = %d", got)
	}
	if proto2pb.Closed_CLOSED_TEN.String() != "CLOSED_TEN" || proto2pb.Closed(5).String() != "5" {
		t.Errorf("enum String() wrong")
	}
}

func TestRequired(t *testing.T) {
	m := &proto2pb.Required{ReqInt32: new(int32(1))}
	if _, err := m.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "req_string") {
		t.Errorf("MarshalBinary error = %v, want missing req_string", err)
	}
	m.ReqString = new("")
	m.InnerMap = map[string]*proto2pb.Required_Inner{"x": {}}
	if _, err := m.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "flag") {
		t.Errorf("MarshalBinary error = %v, want missing nested flag", err)
	}
	m.InnerMap["x"].Flag = new(true)
	b, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var d proto2pb.Required
	if err := d.UnmarshalBinary(b[:0]); err == nil {
		t.Errorf("UnmarshalBinary of empty input succeeded, want missing required error")
	}
	if err := d.UnmarshalBinary(b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&d, m) {
		t.Errorf("round trip mismatch: %+v vs %+v", &d, m)
	}
	// ProtoMerge does not check required fields.
	var p proto2pb.Required
	if err := p.ProtoMerge(nil); err != nil {
		t.Errorf("ProtoMerge: %v", err)
	}
}

func TestUTF8Validation(t *testing.T) {
	m := &proto3pb.Scalars{FString: "\xff"}
	if _, err := m.MarshalBinary(); err == nil {
		t.Error("MarshalBinary accepted invalid UTF-8")
	}
	bad := []byte{0x4a, 0x01, 0xff} // f_string = "\xff"
	var d proto3pb.Scalars
	if err := d.UnmarshalBinary(bad); err == nil {
		t.Error("UnmarshalBinary accepted invalid UTF-8")
	}
	mk := &proto3pb.Maps{MStringString: map[string]string{"\xff": ""}}
	if _, err := mk.MarshalBinary(); err == nil {
		t.Error("MarshalBinary accepted invalid UTF-8 map key")
	}
	e := &editionspb.Features{RequiredInt: new(int32(0)), Unverified: new("\xff")}
	if _, err := e.MarshalBinary(); err != nil {
		t.Errorf("utf8_validation = NONE rejected: %v", err)
	}
	e.Verified = new("\xff")
	if _, err := e.MarshalBinary(); err == nil {
		t.Error("utf8_validation = VERIFY accepted invalid UTF-8")
	}
}

func TestMalformed(t *testing.T) {
	cases := map[string][]byte{
		"truncated varint":   {0x08},
		"truncated tag":      {0x80},
		"field zero":         {0x00, 0x01},
		"truncated bytes":    {0x4a, 0x05, 'a'},
		"truncated fixed64":  {0x09, 1, 2, 3},
		"truncated fixed32":  {0x3d, 1, 2},
		"bad wire type":      {0x0e},
		"end group":          {0x0c},
		"overlong varint":    {0x08, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02},
		"unterminated group": {0xfb, 0x06},
		"nested truncated":   {0x8a, 0x01, 0x02, 0x08},
		"packed truncated":   {0x0a, 0x03, 0x00, 0x00},
	}
	for name, in := range cases {
		var m proto3pb.Scalars
		if err := m.UnmarshalBinary(in); err == nil {
			t.Errorf("%s: UnmarshalBinary(% x) succeeded", name, in)
		}
	}
	var r proto3pb.Repeateds
	if err := r.UnmarshalBinary([]byte{0x0a, 0x03, 0, 0, 0}); err == nil {
		t.Error("packed doubles with bad length accepted")
	}
}

func TestRecursionLimit(t *testing.T) {
	// Build Nested{recursive: Nested{recursive: ...}} deeper than the limit.
	var b []byte
	for i := 0; i < 10001; i++ {
		inner := b
		b = append([]byte{0x12}, appendVarint(nil, uint64(len(inner)))...)
		b = append(b, inner...)
	}
	var m proto3pb.Scalars_Nested
	if err := m.UnmarshalBinary(b); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Errorf("err = %v, want depth error", err)
	}
	// Groups too.
	var g []byte
	for i := 0; i < 10001; i++ {
		g = append(append([]byte{0x33}, g...), 0x34) // field 6 group
	}
	var e proto3pb.Scalars
	if err := e.UnmarshalBinary(g); err == nil {
		t.Error("deep unknown groups accepted")
	}
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func TestMergeSemantics(t *testing.T) {
	a := &proto3pb.Scalars{FInt32: 1, FNested: &proto3pb.Scalars_Nested{A: 1}}
	b := &proto3pb.Scalars{FString: "s", FNested: &proto3pb.Scalars_Nested{Recursive: &proto3pb.Scalars_Nested{A: 2}}}
	ab, _ := a.MarshalBinary()
	bb, _ := b.MarshalBinary()
	var m proto3pb.Scalars
	if err := m.UnmarshalBinary(append(ab, bb...)); err != nil {
		t.Fatal(err)
	}
	want := &proto3pb.Scalars{FInt32: 1, FString: "s", FNested: &proto3pb.Scalars_Nested{A: 1, Recursive: &proto3pb.Scalars_Nested{A: 2}}}
	if !reflect.DeepEqual(&m, want) {
		t.Errorf("merged = %+v, want %+v", &m, want)
	}
	// Oneof: a different member replaces, the same message member merges.
	o1, _ := (&proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CNested{CNested: &proto3pb.Scalars_Nested{A: 1}}}).MarshalBinary()
	o2, _ := (&proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CNested{CNested: &proto3pb.Scalars_Nested{Recursive: &proto3pb.Scalars_Nested{}}}}).MarshalBinary()
	o3, _ := (&proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CString{CString: "last"}}).MarshalBinary()
	var o proto3pb.Oneofs
	if err := o.UnmarshalBinary(append(o1, o2...)); err != nil {
		t.Fatal(err)
	}
	if n := o.GetCNested(); n.GetA() != 1 || n.GetRecursive() == nil {
		t.Errorf("oneof merge = %+v", n)
	}
	if err := o.ProtoMerge(o3); err != nil {
		t.Fatal(err)
	}
	if o.GetCString() != "last" || o.GetCNested() != nil {
		t.Errorf("oneof replace = %+v", &o)
	}
	// Map entries with duplicate keys: last wins.
	m1, _ := (&proto3pb.Maps{MStringString: map[string]string{"k": "1"}}).MarshalBinary()
	m2, _ := (&proto3pb.Maps{MStringString: map[string]string{"k": "2"}}).MarshalBinary()
	var mm proto3pb.Maps
	if err := mm.UnmarshalBinary(append(m1, m2...)); err != nil {
		t.Fatal(err)
	}
	if mm.MStringString["k"] != "2" {
		t.Errorf("map merge = %v", mm.MStringString)
	}
	// Packed and unpacked encodings are both accepted for repeated scalars.
	var r proto3pb.Repeateds
	if err := r.UnmarshalBinary([]byte{0x28, 0x01, 0x2a, 0x02, 0x02, 0x03, 0x28, 0x04}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.RInt32, []int32{1, 2, 3, 4}) {
		t.Errorf("RInt32 = %v", r.RInt32)
	}
}

func TestNilAndEmpty(t *testing.T) {
	var m *proto3pb.Scalars
	if m.ProtoSize() != 0 {
		t.Error("nil ProtoSize != 0")
	}
	b, err := m.MarshalBinary()
	if err != nil || len(b) != 0 {
		t.Errorf("nil MarshalBinary = %v, %v", b, err)
	}
	if m.GetFNested().GetRecursive().GetA() != 0 {
		t.Error("nil getter chain")
	}
	// Nil elements of repeated message fields encode as empty messages.
	r := &proto3pb.Repeateds{RNested: []*proto3pb.Scalars_Nested{nil}}
	b, err = r.MarshalBinary()
	if err != nil || !bytes.Equal(b, []byte{0x8a, 0x01, 0x00}) {
		t.Errorf("nil element = % x, %v", b, err)
	}
	// AppendBinary appends.
	s := &proto3pb.Scalars{FInt32: 1}
	b, err = s.AppendBinary([]byte("xx"))
	if err != nil || string(b) != "xx\x28\x01" {
		t.Errorf("AppendBinary = %q, %v", b, err)
	}
}
