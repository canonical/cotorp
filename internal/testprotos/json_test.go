// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	commonpb "github.com/canonical/cotorp/internal/testprotos/common"
	editionspb "github.com/canonical/cotorp/internal/testprotos/editions"
	jsontestpb "github.com/canonical/cotorp/internal/testprotos/jsontest"
	proto2pb "github.com/canonical/cotorp/internal/testprotos/proto2"
	proto3pb "github.com/canonical/cotorp/internal/testprotos/proto3"
	wktpb "github.com/canonical/cotorp/internal/testprotos/wkt"
)

type jsonMessage interface {
	message
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
}

// normalizeJSON decodes JSON into a generic value with numbers converted to
// float64, so documents can be compared independently of formatting.
func normalizeJSON(t *testing.T, b []byte) any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Fatalf("invalid JSON %s: %v", b, err)
	}
	var norm func(any) any
	norm = func(v any) any {
		switch x := v.(type) {
		case json.Number:
			f, err := strconv.ParseFloat(string(x), 64)
			if err != nil {
				t.Fatalf("bad number %s", x)
			}
			return f
		case map[string]any:
			for k, e := range x {
				x[k] = norm(e)
			}
		case []any:
			for i, e := range x {
				x[i] = norm(e)
			}
		}
		return v
	}
	return norm(v)
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	return reflect.DeepEqual(normalizeJSON(t, a), normalizeJSON(t, b))
}

// --- Optional cross-check against protobuf-go's protojson ------------------

// jsonOracle returns a function that converts between binary and JSON using
// the program named by $COTORP_JSON_ORACLE, or nil if it is not set. The
// program is invoked as `oracle <descriptor set> <message> tojson|fromjson`
// with input on stdin; see AGENTS.md.
func jsonOracle(t *testing.T) func(typ, mode string, in []byte) ([]byte, error) {
	path := os.Getenv("COTORP_JSON_ORACLE")
	if path == "" {
		return nil
	}
	needProtoc(t)
	oracleSetOnce.Do(func() {
		dir, err := os.MkdirTemp("", "cotorp-json-oracle")
		if err != nil {
			oracleSetErr = err
			return
		}
		oracleSet = filepath.Join(dir, "set.pb")
		out, err := exec.Command("protoc", "-Iproto", "--include_imports", "-o", oracleSet,
			"proto3/scalars.proto", "proto2/proto2.proto", "editions/editions.proto", "jsontest/json.proto").CombinedOutput()
		if err != nil {
			oracleSetErr = err
			t.Logf("%s", out)
		}
	})
	if oracleSetErr != nil {
		t.Fatal(oracleSetErr)
	}
	return func(typ, mode string, in []byte) ([]byte, error) {
		cmd := exec.Command(path, oracleSet, typ, mode)
		cmd.Stdin = bytes.NewReader(in)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, &oracleError{strings.TrimSpace(stderr.String())}
		}
		return out, nil
	}
}

type oracleError struct{ msg string }

func (e *oracleError) Error() string { return e.msg }

var (
	oracleSetOnce sync.Once
	oracleSet     string
	oracleSetErr  error
)

// --- Canonical encoding -------------------------------------------------------

type jsonCase struct {
	name string
	typ  string // full proto name, for the oracle
	msg  jsonMessage
	json string
}

func TestJSONMarshal(t *testing.T) {
	oracle := jsonOracle(t)
	neg0 := math.Copysign(0, -1)
	cases := []jsonCase{
		{"scalars-empty", "cotorp.test.proto3.Scalars", &proto3pb.Scalars{}, `{}`},
		{"scalars-full", "cotorp.test.proto3.Scalars", &proto3pb.Scalars{
			FDouble: 1.5, FFloat: -2.25, FInt64: -9000000000, FUint64: math.MaxUint64,
			FInt32: -1, FFixed64: 123, FFixed32: math.MaxUint32, FBool: true,
			FString: "hé\"\\\n\x01<", FBytes: []byte{0, 0xff, 0xfe}, FUint32: 7,
			FSfixed32: -5, FSfixed64: -6, FSint32: math.MinInt32, FSint64: math.MinInt64,
			FEnum:   commonpb.Color_COLOR_BLUE,
			FNested: &proto3pb.Scalars_Nested{A: 1, Recursive: &proto3pb.Scalars_Nested{}},
			FShared: &commonpb.Shared{Label: "x", Value: 3},
		}, `{"fDouble":1.5,"fFloat":-2.25,"fInt64":"-9000000000","fUint64":"18446744073709551615",
			"fInt32":-1,"fFixed64":"123","fFixed32":4294967295,"fBool":true,
			"fString":"hé\"\\\n\u0001<","fBytes":"AP/+","fUint32":7,"fSfixed32":-5,
			"fSfixed64":"-6","fSint32":-2147483648,"fSint64":"-9223372036854775808",
			"fEnum":"COLOR_BLUE","fNested":{"a":1,"recursive":{}},"fShared":{"label":"x","value":"3"}}`},
		{"floats-special", "cotorp.test.proto3.Repeateds", &proto3pb.Repeateds{
			RDouble: []float64{math.NaN(), math.Inf(1), math.Inf(-1), neg0, 1e21, 1e-7},
			RFloat:  []float32{0.1, float32(math.Inf(-1))},
		}, `{"rDouble":["NaN","Infinity","-Infinity",-0,1e21,1e-7],"rFloat":[0.1,"-Infinity"]}`},
		{"unknown-enum-number", "cotorp.test.proto3.Scalars", &proto3pb.Scalars{FEnum: 42}, `{"fEnum":42}`},
		{"optionals-zero", "cotorp.test.proto3.Optionals", &proto3pb.Optionals{
			OInt32: new(int32(0)), OString: new(""), OBytes: []byte{}, OBool: new(false),
			OEnum: commonpb.Color_COLOR_UNSPECIFIED.Enum(), OUint64: new(uint64(0)), ODouble: new(0.0),
		}, `{"oDouble":0,"oUint64":"0","oInt32":0,"oBool":false,"oString":"","oBytes":"","oEnum":"COLOR_UNSPECIFIED"}`},
		{"repeateds", "cotorp.test.proto3.Repeateds", &proto3pb.Repeateds{
			RInt64: []int64{-1, 0}, RBool: []bool{true, false}, RString: []string{"", "a"},
			RBytes: [][]byte{{}, {1}}, REnum: []commonpb.Color{commonpb.Color_COLOR_GREEN, 99},
			RNested: []*proto3pb.Scalars_Nested{{A: 1}, {}}, RUnpacked: []int32{5},
		}, `{"rInt64":["-1","0"],"rBool":[true,false],"rString":["","a"],"rBytes":["","AQ=="],
			"rEnum":["COLOR_GREEN",99],"rNested":[{"a":1},{}],"rUnpacked":[5]}`},
		{"maps", "cotorp.test.proto3.Maps", &proto3pb.Maps{
			MStringString: map[string]string{"a": "1", "": "e"},
			MInt32Int64:   map[int32]int64{-1: 2, 10: 3},
			MBoolBytes:    map[bool][]byte{true: {1}, false: {}},
			MSint64Nested: map[int64]*proto3pb.Scalars_Nested{-7: {A: 7}},
			MFixed32Enum:  map[uint32]commonpb.Color{1: commonpb.Color_COLOR_RED},
			MUint64Double: map[uint64]float64{math.MaxUint64: math.Inf(1)},
			MStringShared: map[string]*commonpb.Shared{"k": {}},
		}, `{"mStringString":{"":"e","a":"1"},"mInt32Int64":{"-1":"2","10":"3"},
			"mBoolBytes":{"false":"","true":"AQ=="},"mSint64Nested":{"-7":{"a":7}},
			"mFixed32Enum":{"1":"COLOR_RED"},"mUint64Double":{"18446744073709551615":"Infinity"},
			"mStringShared":{"k":{}}}`},
		{"oneof-zero-member", "cotorp.test.proto3.Oneofs", &proto3pb.Oneofs{
			Choice: &proto3pb.Oneofs_CInt32{}, Other: &proto3pb.Oneofs_OFixed32{OFixed32: 1},
		}, `{"cInt32":0,"oFixed32":1}`},
		{"oneof-message", "cotorp.test.proto3.Oneofs", &proto3pb.Oneofs{
			Choice: &proto3pb.Oneofs_CNested{CNested: &proto3pb.Scalars_Nested{}},
		}, `{"cNested":{}}`},
		{"proto2-groups-defaults", "cotorp.test.proto2.Groups", &proto2pb.Groups{
			Optgroup: &proto2pb.Groups_OptGroup{A: new(int32(1))},
			Repgroup: []*proto2pb.Groups_RepGroup{{Deep: &proto2pb.Groups_RepGroup_Deep{D: new(int64(2))}}},
		}, `{"optgroup":{"a":1},"repgroup":[{"deep":{"d":"2"}}]}`},
		{"proto2-closed-enums", "cotorp.test.proto2.ClosedEnums", &proto2pb.ClosedEnums{
			Single: proto2pb.Closed_CLOSED_TEN.Enum(),
			ByKey:  map[int32]proto2pb.ClosedZero{3: proto2pb.ClosedZero_CLOSED_ZERO_ONE},
		}, `{"single":"CLOSED_TEN","byKey":{"3":"CLOSED_ZERO_ONE"}}`},
		{"editions", "cotorp.test.editions.Features", &editionspb.Features{
			RequiredInt: new(int32(1)), ImplicitInt: 0, Delimited: &editionspb.Features_Child{X: new(int32(2))},
			ClosedEnum: editionspb.ClosedEnum_CLOSED_TWO.Enum(), WithDefault: new("d"),
		}, `{"requiredInt":1,"delimited":{"x":2},"closedEnum":"CLOSED_TWO","withDefault":"d"}`},
		{"names", "cotorp.test.json.Names", &jsontestpb.Names{
			Custom: 1, SnakeCaseField: 2, XLeading: 3, With_2Digits: 4, Color: commonpb.Color_COLOR_RED,
			ByBool: map[bool]string{true: "t"}, ByU64: map[uint64]int32{1: 2}, ByS32: map[int32][]byte{-1: {2}},
		}, `{"renamed":1,"snakeCaseField":2,"Leading":3,"with2Digits":4,"color":"COLOR_RED",
			"byBool":{"true":"t"},"byU64":{"1":2},"byS32":{"-1":"Ag=="}}`},
		{"wkt-empty", "cotorp.test.json.WellKnown", &jsontestpb.WellKnown{}, `{}`},
		{"wkt", "cotorp.test.json.WellKnown", &jsontestpb.WellKnown{
			Ts:    &wktpb.Timestamp{Seconds: 1, Nanos: 20000000},
			Dur:   &wktpb.Duration{Seconds: -1, Nanos: -500},
			Dbl:   &wktpb.DoubleValue{Value: math.NaN()},
			Flt:   &wktpb.FloatValue{},
			I64:   &wktpb.Int64Value{Value: -5},
			U64:   &wktpb.UInt64Value{Value: 5},
			I32:   &wktpb.Int32Value{Value: -3},
			U32:   &wktpb.UInt32Value{Value: 3},
			Bool:  &wktpb.BoolValue{},
			Str:   &wktpb.StringValue{Value: "s"},
			Bytes: &wktpb.BytesValue{Value: []byte("hi")},
			St: &wktpb.Struct{Fields: map[string]*wktpb.Value{
				"n": {Kind: &wktpb.Value_NullValue{}},
				"l": {Kind: &wktpb.Value_ListValue{ListValue: &wktpb.ListValue{Values: []*wktpb.Value{
					{Kind: &wktpb.Value_NumberValue{NumberValue: 1.5}},
					{Kind: &wktpb.Value_StringValue{StringValue: "x"}},
					{Kind: &wktpb.Value_BoolValue{BoolValue: true}},
					{Kind: &wktpb.Value_StructValue{StructValue: &wktpb.Struct{}}},
				}}}},
			}},
			Val:       &wktpb.Value{Kind: &wktpb.Value_StringValue{StringValue: "v"}},
			Lst:       &wktpb.ListValue{},
			Mask:      &wktpb.FieldMask{Paths: []string{"a.b_c", "d"}},
			Empty:     &wktpb.Empty{},
			NullValue: wktpb.NullValue_NULL_VALUE,
			Vals:      []*wktpb.Value{{Kind: &wktpb.Value_NullValue{}}},
			ValMap:    map[string]*wktpb.Value{"k": {Kind: &wktpb.Value_NullValue{}}},
			OptNull:   wktpb.NullValue_NULL_VALUE.Enum(),
			O:         &jsontestpb.WellKnown_ONull{},
			Tss:       []*wktpb.Timestamp{{Seconds: -62135596800}, {Seconds: 253402300799, Nanos: 999999999}},
			Durs:      map[int32]*wktpb.Duration{1: {Seconds: 3, Nanos: 1000}, 2: {Nanos: -1000000}},
		}, `{"ts":"1970-01-01T00:00:01.020Z","dur":"-1.000000500s","dbl":"NaN","flt":0,
			"i64":"-5","u64":"5","i32":-3,"u32":3,"bool":false,"str":"s","bytes":"aGk=",
			"st":{"l":[1.5,"x",true,{}],"n":null},"val":"v","lst":[],"mask":"a.bC,d","empty":{},
			"vals":[null],"valMap":{"k":null},"optNull":null,"oNull":null,
			"tss":["0001-01-01T00:00:00Z","9999-12-31T23:59:59.999999999Z"],
			"durs":{"1":"3.000001s","2":"-0.001s"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.msg.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			if !sameJSON(t, got, []byte(tc.json)) {
				t.Errorf("MarshalJSON:\n got: %s\nwant: %s", got, tc.json)
			}
			// Decoding the expected JSON must reproduce the message.
			dec := newOf(tc.msg).(jsonMessage)
			if err := dec.UnmarshalJSON([]byte(tc.json)); err != nil {
				t.Fatalf("UnmarshalJSON: %v", err)
			}
			wantBin, _ := tc.msg.MarshalBinary()
			gotBin, _ := dec.MarshalBinary()
			if !bytes.Equal(gotBin, wantBin) {
				t.Errorf("UnmarshalJSON produced a different message\n got: % x\nwant: % x", gotBin, wantBin)
			}
			if oracle == nil {
				return
			}
			oj, err := oracle(tc.typ, "tojson", wantBin)
			if err != nil {
				t.Fatalf("oracle tojson: %v", err)
			}
			if !sameJSON(t, oj, got) {
				t.Errorf("protojson disagrees:\nprotojson: %s\n   cotorp: %s", oj, got)
			}
			ob, err := oracle(tc.typ, "fromjson", got)
			if err != nil {
				t.Fatalf("oracle fromjson: %v", err)
			}
			re := newOf(tc.msg)
			if err := re.UnmarshalBinary(ob); err != nil {
				t.Fatal(err)
			}
			if reb, _ := re.MarshalBinary(); !bytes.Equal(reb, wantBin) {
				t.Errorf("protojson decoded our JSON differently\n got: % x\nwant: % x", reb, wantBin)
			}
		})
	}
}

// --- Parsing ------------------------------------------------------------------

type jsonParseCase struct {
	name string
	typ  string
	in   string
	want jsonMessage // nil means the input must be rejected
}

// jsonDeviations lists parse cases where cotorp deliberately differs from
// protojson; the oracle comparison is skipped for them.
var jsonDeviations = map[string]string{
	"null message": "encoding/json expects Unmarshalers to treat a top-level null as a no-op; protojson rejects it",
}

func TestJSONUnmarshal(t *testing.T) {
	oracle := jsonOracle(t)
	const s3 = "cotorp.test.proto3.Scalars"
	const r3 = "cotorp.test.proto3.Repeateds"
	const o3 = "cotorp.test.proto3.Oneofs"
	const m3 = "cotorp.test.proto3.Maps"
	const wk = "cotorp.test.json.WellKnown"
	const nm = "cotorp.test.json.Names"
	scalars := func(m *proto3pb.Scalars) jsonMessage { return m }
	cases := []jsonParseCase{
		// Field names.
		{"proto name", s3, `{"f_int32": 1}`, scalars(&proto3pb.Scalars{FInt32: 1})},
		{"json name", s3, `{"fInt32": 1}`, scalars(&proto3pb.Scalars{FInt32: 1})},
		{"custom json name", nm, `{"renamed": 1}`, &jsontestpb.Names{Custom: 1}},
		{"custom json name via proto name", nm, `{"custom": 1}`, &jsontestpb.Names{Custom: 1}},
		{"duplicate via both names", s3, `{"fInt32": 1, "f_int32": 2}`, nil},
		{"duplicate", s3, `{"fInt32": 1, "fInt32": 2}`, nil},
		{"unknown field", s3, `{"nope": 1}`, nil},
		{"whitespace", s3, " \n{ \"fInt32\" : 1 } \n", scalars(&proto3pb.Scalars{FInt32: 1})},
		{"null message", s3, `null`, scalars(&proto3pb.Scalars{})},
		{"not an object", s3, `[]`, nil},
		{"trailing data", s3, `{} {}`, nil},
		{"null field", s3, `{"fInt32": null, "fNested": null, "fString": null}`, scalars(&proto3pb.Scalars{})},

		// Integers.
		{"int quoted", s3, `{"fInt32": "-5", "fInt64": "7", "fUint64": "18446744073709551615"}`,
			scalars(&proto3pb.Scalars{FInt32: -5, FInt64: 7, FUint64: math.MaxUint64})},
		{"int64 unquoted", s3, `{"fInt64": -9007199254740993}`, scalars(&proto3pb.Scalars{FInt64: -9007199254740993})},
		{"int exponent", s3, `{"fInt32": 1e2, "fUint32": "1.5e1", "fInt64": 1.0}`,
			scalars(&proto3pb.Scalars{FInt32: 100, FUint32: 15, FInt64: 1})},
		{"int fraction", s3, `{"fInt32": 1.5}`, nil},
		{"int32 overflow", s3, `{"fInt32": 2147483648}`, nil},
		{"int32 overflow exponent", s3, `{"fInt32": 3e9}`, nil},
		{"uint negative", s3, `{"fUint32": -1}`, nil},
		{"int huge exponent", s3, `{"fInt64": 1e999999999}`, nil},
		{"int from bool", s3, `{"fInt32": true}`, nil},
		{"int quoted space", s3, `{"fInt32": " 1"}`, nil},
		{"int quoted plus", s3, `{"fInt32": "+1"}`, nil},
		{"int quoted leading zero", s3, `{"fInt32": "01"}`, nil},
		{"int quoted hex", s3, `{"fInt32": "0x10"}`, nil},

		// Floats.
		{"float specials", r3, `{"rDouble": ["NaN", "Infinity", "-Infinity", "1.5", -0, 1e308]}`, nil /* checked below */},
		{"float quoted number", s3, `{"fDouble": "2.5", "fFloat": "-1"}`, scalars(&proto3pb.Scalars{FDouble: 2.5, FFloat: -1})},
		{"float32 overflow", s3, `{"fFloat": 1e39}`, nil},
		{"double overflow", s3, `{"fDouble": "1e309"}`, nil},
		{"float bad string", s3, `{"fDouble": "nan"}`, nil},

		// Bools, strings and bytes.
		{"bool quoted", s3, `{"fBool": "true"}`, nil},
		{"string escapes", s3, `{"fString": "aé\n\"😀"}`, scalars(&proto3pb.Scalars{FString: "aé\n\"\U0001F600"})},
		{"string from number", s3, `{"fString": 1}`, nil},
		{"bytes std", s3, `{"fBytes": "AP/+"}`, scalars(&proto3pb.Scalars{FBytes: []byte{0, 0xff, 0xfe}})},
		{"bytes url", s3, `{"fBytes": "AP_-"}`, scalars(&proto3pb.Scalars{FBytes: []byte{0, 0xff, 0xfe}})},
		{"bytes unpadded", s3, `{"fBytes": "AQ"}`, scalars(&proto3pb.Scalars{FBytes: []byte{1}})},
		{"bytes invalid", s3, `{"fBytes": "!!"}`, nil},

		// Enums.
		{"enum name", s3, `{"fEnum": "COLOR_RED"}`, scalars(&proto3pb.Scalars{FEnum: commonpb.Color_COLOR_RED})},
		{"enum number", s3, `{"fEnum": 2}`, scalars(&proto3pb.Scalars{FEnum: commonpb.Color_COLOR_GREEN})},
		{"enum unknown number", s3, `{"fEnum": 77}`, scalars(&proto3pb.Scalars{FEnum: 77})},
		{"enum unknown name", s3, `{"fEnum": "PURPLE"}`, nil},

		// Repeated fields and maps.
		{"repeated null", r3, `{"rInt32": null}`, &proto3pb.Repeateds{}},
		{"repeated element null", r3, `{"rInt32": [1, null]}`, nil},
		{"repeated not array", r3, `{"rInt32": 1}`, nil},
		{"map int key", m3, `{"mInt32Int64": {"-3": "4", "5": 6}}`, &proto3pb.Maps{MInt32Int64: map[int32]int64{-3: 4, 5: 6}}},
		{"map bad key", m3, `{"mInt32Int64": {"x": 1}}`, nil},
		{"map bool key", m3, `{"mBoolBytes": {"true": "AQ=="}}`, &proto3pb.Maps{MBoolBytes: map[bool][]byte{true: {1}}}},
		{"map bad bool key", m3, `{"mBoolBytes": {"True": ""}}`, nil},
		{"map null value", m3, `{"mStringString": {"a": null}}`, nil},

		// Oneofs.
		{"oneof", o3, `{"cString": "x"}`, &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CString{CString: "x"}}},
		{"oneof two members", o3, `{"cString": "x", "cInt32": 1}`, nil},
		{"oneof null then value", o3, `{"cString": null, "cInt32": 1}`, &proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CInt32{CInt32: 1}}},

		// Well-known types.
		{"timestamp offset", wk, `{"ts": "1970-01-01T01:00:00.5+01:00"}`, &jsontestpb.WellKnown{Ts: &wktpb.Timestamp{Nanos: 500000000}}},
		{"timestamp invalid", wk, `{"ts": "1970-01-01 00:00:00Z"}`, nil},
		{"timestamp out of range", wk, `{"ts": "0000-12-31T23:59:59Z"}`, nil},
		{"duration", wk, `{"dur": "-3.5s"}`, &jsontestpb.WellKnown{Dur: &wktpb.Duration{Seconds: -3, Nanos: -500000000}}},
		{"duration no unit", wk, `{"dur": "3"}`, nil},
		{"duration too precise", wk, `{"dur": "1.0000000001s"}`, nil},
		{"duration out of range", wk, `{"dur": "315576000001s"}`, nil},
		{"wrappers", wk, `{"i64": "5", "u32": 3, "bool": true, "str": "s", "dbl": "Infinity"}`, &jsontestpb.WellKnown{
			I64: &wktpb.Int64Value{Value: 5}, U32: &wktpb.UInt32Value{Value: 3}, Bool: &wktpb.BoolValue{Value: true},
			Str: &wktpb.StringValue{Value: "s"}, Dbl: &wktpb.DoubleValue{Value: math.Inf(1)}}},
		{"wrapper null is unset", wk, `{"i64": null}`, &jsontestpb.WellKnown{}},
		{"value null", wk, `{"val": null}`, &jsontestpb.WellKnown{Val: &wktpb.Value{Kind: &wktpb.Value_NullValue{}}}},
		{"value number", wk, `{"val": 2}`, &jsontestpb.WellKnown{Val: &wktpb.Value{Kind: &wktpb.Value_NumberValue{NumberValue: 2}}}},
		{"value nested", wk, `{"val": {"a": [null, {"b": false}]}}`, &jsontestpb.WellKnown{Val: &wktpb.Value{Kind: &wktpb.Value_StructValue{StructValue: &wktpb.Struct{Fields: map[string]*wktpb.Value{
			"a": {Kind: &wktpb.Value_ListValue{ListValue: &wktpb.ListValue{Values: []*wktpb.Value{
				{Kind: &wktpb.Value_NullValue{}},
				{Kind: &wktpb.Value_StructValue{StructValue: &wktpb.Struct{Fields: map[string]*wktpb.Value{"b": {Kind: &wktpb.Value_BoolValue{}}}}}},
			}}}},
		}}}}}},
		{"repeated values with null", wk, `{"vals": [null, 1]}`, &jsontestpb.WellKnown{Vals: []*wktpb.Value{
			{Kind: &wktpb.Value_NullValue{}}, {Kind: &wktpb.Value_NumberValue{NumberValue: 1}}}}},
		{"null value enum", wk, `{"nullValue": null, "optNull": null}`, &jsontestpb.WellKnown{OptNull: wktpb.NullValue_NULL_VALUE.Enum()}},
		{"oneof null value member", wk, `{"oNull": null}`, &jsontestpb.WellKnown{O: &jsontestpb.WellKnown_ONull{}}},
		{"oneof value member null", wk, `{"oVal": null}`, &jsontestpb.WellKnown{O: &jsontestpb.WellKnown_OVal{OVal: &wktpb.Value{Kind: &wktpb.Value_NullValue{}}}}},
		{"struct", wk, `{"st": {}}`, &jsontestpb.WellKnown{St: &wktpb.Struct{}}},
		{"struct not object", wk, `{"st": []}`, nil},
		{"list", wk, `{"lst": [1]}`, &jsontestpb.WellKnown{Lst: &wktpb.ListValue{Values: []*wktpb.Value{{Kind: &wktpb.Value_NumberValue{NumberValue: 1}}}}}},
		{"field mask", wk, `{"mask": "a.bC,d"}`, &jsontestpb.WellKnown{Mask: &wktpb.FieldMask{Paths: []string{"a.b_c", "d"}}}},
		{"field mask empty", wk, `{"mask": ""}`, &jsontestpb.WellKnown{Mask: &wktpb.FieldMask{}}},
		{"field mask underscore", wk, `{"mask": "a_b"}`, nil},
		{"empty", wk, `{"empty": {}}`, &jsontestpb.WellKnown{Empty: &wktpb.Empty{}}},

		// proto2 and editions.
		{"required missing", "cotorp.test.proto2.Required", `{"reqInt32": 1}`, nil},
		{"required present", "cotorp.test.proto2.Required", `{"reqInt32": 1, "reqString": ""}`,
			&proto2pb.Required{ReqInt32: new(int32(1)), ReqString: new("")}},
		{"group by field name", "cotorp.test.proto2.Groups", `{"optgroup": {"a": 1}}`,
			&proto2pb.Groups{Optgroup: &proto2pb.Groups_OptGroup{A: new(int32(1))}}},
		// Like protojson, numeric values of closed enums are not checked.
		{"closed enum unknown number", "cotorp.test.proto2.ClosedEnums", `{"single": 5}`,
			&proto2pb.ClosedEnums{Single: proto2pb.Closed(5).Enum()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var target jsonMessage
			switch {
			case tc.want != nil:
				target = newOf(tc.want).(jsonMessage)
			default:
				target = messageForType(t, tc.typ)
			}
			err := target.UnmarshalJSON([]byte(tc.in))
			if tc.name == "float specials" {
				r := target.(*proto3pb.Repeateds)
				if err != nil || len(r.RDouble) != 6 || !math.IsNaN(r.RDouble[0]) || !math.IsInf(r.RDouble[1], 1) ||
					!math.IsInf(r.RDouble[2], -1) || r.RDouble[3] != 1.5 || r.RDouble[4] != 0 || !math.Signbit(r.RDouble[4]) || r.RDouble[5] != 1e308 {
					t.Fatalf("got %v, %v", r.RDouble, err)
				}
				return
			}
			switch {
			case tc.want == nil && err == nil:
				t.Errorf("accepted invalid input")
			case tc.want != nil && err != nil:
				t.Errorf("UnmarshalJSON: %v", err)
			case tc.want != nil:
				got, _ := target.MarshalBinary()
				want, _ := tc.want.MarshalBinary()
				if !bytes.Equal(got, want) {
					t.Errorf("decoded\n got: % x\nwant: % x", got, want)
				}
			}
			if oracle == nil || jsonDeviations[tc.name] != "" {
				return
			}
			ob, oerr := oracle(tc.typ, "fromjson", []byte(tc.in))
			switch {
			case (oerr == nil) != (err == nil):
				t.Errorf("protojson disagrees on validity: protojson err = %v, cotorp err = %v", oerr, err)
			case err == nil:
				re := newOf(target)
				if err := re.UnmarshalBinary(ob); err != nil {
					t.Fatal(err)
				}
				a, _ := re.MarshalBinary()
				b, _ := target.MarshalBinary()
				if !bytes.Equal(a, b) {
					t.Errorf("protojson decoded differently\nprotojson: % x\n   cotorp: % x", a, b)
				}
			}
		})
	}
}

func messageForType(t *testing.T, typ string) jsonMessage {
	switch typ {
	case "cotorp.test.proto3.Scalars":
		return &proto3pb.Scalars{}
	case "cotorp.test.proto3.Repeateds":
		return &proto3pb.Repeateds{}
	case "cotorp.test.proto3.Oneofs":
		return &proto3pb.Oneofs{}
	case "cotorp.test.proto3.Maps":
		return &proto3pb.Maps{}
	case "cotorp.test.json.WellKnown":
		return &jsontestpb.WellKnown{}
	case "cotorp.test.json.Names":
		return &jsontestpb.Names{}
	case "cotorp.test.proto2.Required":
		return &proto2pb.Required{}
	case "cotorp.test.proto2.ClosedEnums":
		return &proto2pb.ClosedEnums{}
	}
	t.Fatalf("no message for %s", typ)
	return nil
}

func TestJSONMarshalErrors(t *testing.T) {
	cases := map[string]jsonMessage{
		"invalid utf8":         &proto3pb.Scalars{FString: "\xff"},
		"invalid utf8 map key": &proto3pb.Maps{MStringString: map[string]string{"\xff": ""}},
		"missing required":     &proto2pb.Required{},
		"timestamp range":      &jsontestpb.WellKnown{Ts: &wktpb.Timestamp{Seconds: 253402300800}},
		"timestamp nanos":      &jsontestpb.WellKnown{Ts: &wktpb.Timestamp{Nanos: -1}},
		"duration sign":        &jsontestpb.WellKnown{Dur: &wktpb.Duration{Seconds: 1, Nanos: -1}},
		"duration range":       &jsontestpb.WellKnown{Dur: &wktpb.Duration{Seconds: 315576000001}},
		"value nan":            &jsontestpb.WellKnown{Val: &wktpb.Value{Kind: &wktpb.Value_NumberValue{NumberValue: math.NaN()}}},
		"value unset":          &jsontestpb.WellKnown{Val: &wktpb.Value{}},
		"field mask uppercase": &jsontestpb.WellKnown{Mask: &wktpb.FieldMask{Paths: []string{"aB"}}},
		"field mask __":        &jsontestpb.WellKnown{Mask: &wktpb.FieldMask{Paths: []string{"a__b"}}},
		"any":                  &jsontestpb.WellKnown{Any: &wktpb.Any{TypeUrl: "type.googleapis.com/x"}},
	}
	for name, m := range cases {
		if b, err := m.MarshalJSON(); err == nil {
			t.Errorf("%s: MarshalJSON succeeded: %s", name, b)
		}
	}
}

// TestJSONStdlib checks the generated methods integrate with encoding/json.
func TestJSONStdlib(t *testing.T) {
	type wrapper struct {
		M *proto3pb.Scalars  `json:"m"`
		L []*commonpb.Shared `json:"l"`
		N *wktpb.Timestamp   `json:"n"`
	}
	in := wrapper{M: &proto3pb.Scalars{FInt64: 1}, L: []*commonpb.Shared{{Label: "a"}}, N: &wktpb.Timestamp{}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"m":{"fInt64":"1"},"l":[{"label":"a"}],"n":"1970-01-01T00:00:00Z"}`; string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", b, want)
	}
	var out wrapper
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.M.FInt64 != 1 || out.L[0].Label != "a" || out.N == nil {
		t.Errorf("json.Unmarshal = %+v", out)
	}
}
