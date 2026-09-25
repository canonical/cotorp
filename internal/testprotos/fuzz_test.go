// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"testing"

	"github.com/canonical/cotorp/internal/testprotos/descriptorpb"
	editionspb "github.com/canonical/cotorp/internal/testprotos/editions"
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
		}
	})
}
