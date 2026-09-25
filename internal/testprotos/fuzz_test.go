// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"testing"

	"github.com/canonical/cotorp/internal/testprotos/descriptorpb"
	editionspb "github.com/canonical/cotorp/internal/testprotos/editions"
	jsontestpb "github.com/canonical/cotorp/internal/testprotos/jsontest"
	proto2pb "github.com/canonical/cotorp/internal/testprotos/proto2"
	proto3pb "github.com/canonical/cotorp/internal/testprotos/proto3"
)

// fuzzMessages are decoded from every fuzz input.
func fuzzMessages() []message {
	return []message{
		&proto3pb.Scalars{}, &proto3pb.Optionals{}, &proto3pb.Repeateds{},
		&proto3pb.Maps{}, &proto3pb.Oneofs{},
		&proto2pb.Defaults{}, &proto2pb.Required{}, &proto2pb.Groups{},
		&proto2pb.ClosedEnums{}, &proto2pb.Packed{}, &proto2pb.Extendable{},
		&editionspb.Features{}, &editionspb.ImplicitMessage{},
		&descriptorpb.FileDescriptorSet{},
		&jsontestpb.WellKnown{}, &jsontestpb.Names{},
	}
}

func FuzzRoundTrip(f *testing.F) {
	seeds := []message{
		&proto3pb.Scalars{FInt32: -1, FString: "x", FNested: &proto3pb.Scalars_Nested{A: 1}},
		&proto3pb.Repeateds{RInt32: []int32{1, -1}, RString: []string{"a"}, RDouble: []float64{1}},
		&proto3pb.Maps{MStringString: map[string]string{"a": "b"}, MSint64Nested: map[int64]*proto3pb.Scalars_Nested{1: {}}},
		&proto3pb.Oneofs{Choice: &proto3pb.Oneofs_CNested{CNested: &proto3pb.Scalars_Nested{A: 2}}},
		&proto2pb.Groups{Optgroup: &proto2pb.Groups_OptGroup{A: new(int32(1))}, Repgroup: []*proto2pb.Groups_RepGroup{{Deep: &proto2pb.Groups_RepGroup_Deep{}}}},
		&proto2pb.ClosedEnums{Rep: []proto2pb.Closed{1, 2}, ByKey: map[int32]proto2pb.ClosedZero{1: 1}},
		&editionspb.Features{RequiredInt: new(int32(1)), Delimited: &editionspb.Features_Child{}},
	}
	// Regression: an implicit NullValue field holding an undeclared number.
	f.Add([]byte("\x88\x010"))
	for _, s := range seeds {
		b, err := s.MarshalBinary()
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, proto := range fuzzMessages() {
			m := newOf(proto)
			if err := m.UnmarshalBinary(data); err != nil {
				continue
			}
			out, err := m.MarshalBinary()
			if err != nil {
				t.Fatalf("%T: decoded but failed to encode: %v", m, err)
			}
			if len(out) != m.ProtoSize() {
				t.Fatalf("%T: ProtoSize %d != len %d", m, m.ProtoSize(), len(out))
			}
			m2 := newOf(proto)
			if err := m2.UnmarshalBinary(out); err != nil {
				t.Fatalf("%T: re-decode failed: %v", m, err)
			}
			out2, err := m2.MarshalBinary()
			if err != nil {
				t.Fatalf("%T: re-encode failed: %v", m, err)
			}
			if !bytes.Equal(out, out2) {
				t.Fatalf("%T: encoding not stable\n% x\n% x", m, out, out2)
			}
			checkJSONRoundTrip(t, m.(jsonMessage))
		}
	})
}

// checkJSONRoundTrip checks that m's JSON encoding, if it has one, decodes
// successfully and that JSON encoding is a fixed point after one round trip.
// The first round may legitimately lose information that ProtoJSON cannot
// represent (unknown fields, or out-of-range NullValue numbers, which encode
// as null exactly as in protojson).
func checkJSONRoundTrip(t *testing.T, m jsonMessage) {
	j1, err := m.MarshalJSON()
	if err != nil {
		return // e.g. invalid UTF-8 in proto2 strings, or out-of-range timestamps
	}
	var prev []byte
	for i, j := 0, j1; i < 2; i++ {
		m2 := newOf(m).(jsonMessage)
		if err := m2.UnmarshalJSON(j); err != nil {
			t.Fatalf("%T: failed to decode own JSON %s: %v", m, j, err)
		}
		next, err := m2.MarshalJSON()
		if err != nil {
			t.Fatalf("%T: re-encode JSON of %s: %v", m, j, err)
		}
		if prev != nil && !bytes.Equal(prev, next) {
			t.Fatalf("%T: JSON not stable\n%s\n%s", m, prev, next)
		}
		prev, j = next, next
	}
}

func FuzzJSON(f *testing.F) {
	for _, s := range []string{
		`{}`, `null`, `{"fInt32": 1, "fString": "x", "fNested": {"a": 2}}`,
		`{"rDouble": ["NaN", 1e3, -0], "rInt64": ["1", 2]}`,
		`{"mInt32Int64": {"-1": "2"}, "mBoolBytes": {"true": "AQ=="}}`,
		`{"cNested": {"a": 1}}`, `{"optgroup": {"a": 1}}`,
		`{"ts": "1970-01-01T00:00:00Z", "dur": "-1.5s", "val": {"a": [null, true, 1, "s"]}}`,
		`{"mask": "a.bC", "i64": "5", "st": {"k": {}}, "renamed": 1, "byS32": {"-1": "Ag=="}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, proto := range fuzzMessages() {
			m := newOf(proto).(jsonMessage)
			if err := m.UnmarshalJSON(data); err != nil {
				continue
			}
			bin, err := m.MarshalBinary()
			if err != nil {
				t.Fatalf("%T: decoded JSON but failed to encode binary: %v", m, err)
			}
			m2 := newOf(proto).(jsonMessage)
			if err := m2.UnmarshalBinary(bin); err != nil {
				t.Fatalf("%T: binary round trip: %v", m, err)
			}
			checkJSONRoundTrip(t, m)
		}
	})
}
