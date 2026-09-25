// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"strings"
	"testing"

	otlppb "github.com/canonical/cotorp/internal/testprotos/otlp"
	wktpb "github.com/canonical/cotorp/internal/testprotos/wkt"
)

// The otlp package is generated with -json_enum_numbers,
// -json_discard_unknown and -json_hex for the Span and Link ID fields (see
// gen.go).

func otlpSpan() *otlppb.Span {
	return &otlppb.Span{
		TraceId:           []byte{0x5b, 0x8e, 0xff, 0xf7, 0x98, 0x03, 0x81, 0x03, 0xd2, 0x69, 0xb6, 0x33, 0x81, 0x3f, 0xc6, 0x0c},
		SpanId:            []byte{0xee, 0xe1, 0x9b, 0x7e, 0xc3, 0xc1, 0xb1, 0x74},
		ParentSpanId:      []byte{0xaa, 0xbb},
		Name:              "op",
		Kind:              otlppb.Span_SPAN_KIND_SERVER,
		StartTimeUnixNano: 1544712660000000000,
		Links:             []*otlppb.Span_Link{{TraceId: []byte{0x01}, SpanId: []byte{0xff}}},
		Status:            &otlppb.Status{Code: otlppb.Status_STATUS_CODE_ERROR},
		HexList:           [][]byte{{0xab}, {}},
		Plain:             []byte{0xff},
		NullValue:         wktpb.NullValue_NULL_VALUE.Enum(),
		Codes:             map[string]otlppb.Status_StatusCode{"a": otlppb.Status_STATUS_CODE_OK},
	}
}

const otlpSpanJSON = `{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174",` +
	`"parentSpanId":"aabb","name":"op","kind":2,"startTimeUnixNano":"1544712660000000000",` +
	`"links":[{"traceId":"01","spanId":"ff"}],"status":{"code":2},"hexList":["ab",""],` +
	`"plain":"/w==","nullValue":null,"codes":{"a":1}}`

func TestJSONOptionsMarshal(t *testing.T) {
	got, err := otlpSpan().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != otlpSpanJSON {
		t.Errorf("MarshalJSON:\n got: %s\nwant: %s", got, otlpSpanJSON)
	}
	// NullValue stays null under -json_enum_numbers (see nullValue above),
	// and bytes fields not listed with -json_hex keep base64.
	got, err = (&otlppb.AnyValue{Value: &otlppb.AnyValue_BytesValue{BytesValue: []byte{0xff}}}).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"bytesValue":"/w=="}`; string(got) != want {
		t.Errorf("AnyValue: got %s, want %s", got, want)
	}
}

func TestJSONOptionsUnmarshal(t *testing.T) {
	want, _ := otlpSpan().MarshalBinary()

	// Round trip of our own output.
	var s otlppb.Span
	if err := s.UnmarshalJSON([]byte(otlpSpanJSON)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MarshalBinary(); !bytes.Equal(got, want) {
		t.Errorf("round trip mismatch\n got: % x\nwant: % x", got, want)
	}

	// Upper-case hex, enum names, and unknown keys (with arbitrary values,
	// at every level) are accepted.
	in := `{"traceId":"5B8EFFF798038103D269B633813FC60C","spanId":"EEE19b7ec3c1b174",` +
		`"parentSpanId":"AaBb","name":"op","kind":"SPAN_KIND_SERVER",` +
		`"startTimeUnixNano":1544712660000000000,"droppedAttributesCount":0,` +
		`"links":[{"traceId":"01","spanId":"FF","attributes":[{"key":"k"}]}],` +
		`"status":{"code":"STATUS_CODE_ERROR","future":{"x":[1,2]}},"hexList":["AB",""],` +
		`"plain":"/w==","nullValue":null,"codes":{"a":"STATUS_CODE_OK"},"unknown":null}`
	var s2 otlppb.Span
	if err := s2.UnmarshalJSON([]byte(in)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.MarshalBinary(); !bytes.Equal(got, want) {
		t.Errorf("decoded mismatch\n got: % x\nwant: % x", got, want)
	}
	var n otlppb.Nothing
	if err := n.UnmarshalJSON([]byte(`{"a":1,"b":{"c":[]}}`)); err != nil {
		t.Errorf("empty message with unknown keys: %v", err)
	}

	bad := map[string]string{
		"odd-length hex":      `{"traceId":"abc"}`,
		"non-hex":             `{"spanId":"zz"}`,
		"base64 in hex field": `{"traceId":"/w=="}`,
		"hex not string":      `{"traceId":12}`,
		"duplicate key":       `{"name":"a","name":"b"}`,
		"invalid unknown":     `{"unknown":}`,
		"not an object":       `[]`,
	}
	for name, in := range bad {
		var s otlppb.Span
		if err := s.UnmarshalJSON([]byte(in)); err == nil {
			t.Errorf("%s: accepted %s", name, in)
		}
	}
}

// TestJSONOptionsDefaultsUnchanged checks that packages generated without
// the options keep the canonical behaviour.
func TestJSONOptionsDefaultsUnchanged(t *testing.T) {
	var w wktpb.Struct
	if err := w.UnmarshalJSON([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := (&wktpb.Value{Kind: &wktpb.Value_NullValue{}}).MarshalJSON()
	if string(got) != "null" {
		t.Errorf("Value null = %s", got)
	}
	var e wktpb.Empty
	if err := e.UnmarshalJSON([]byte(`{"nope":1}`)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("default package should reject unknown keys, got %v", err)
	}
}
