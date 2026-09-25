// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package testprotos_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/canonical/cotorp/internal/testprotos/descriptorpb"
)

// TestDescriptorRoundTrip decodes protoc's FileDescriptorSet output with the
// cotorp-generated descriptor.proto code and checks that re-encoding it
// reproduces protoc's bytes exactly. descriptor.proto exercises proto2
// optional/repeated fields, nested messages, closed enums, defaults,
// extension ranges and packed fields.
func TestDescriptorRoundTrip(t *testing.T) {
	needProtoc(t)
	out := filepath.Join(t.TempDir(), "set.pb")
	cmd := exec.Command("protoc", "-Iproto", "--include_imports", "--include_source_info",
		"--descriptor_set_out="+out,
		"proto3/scalars.proto", "proto2/proto2.proto", "editions/editions.proto",
		"editions/editions2024.proto", "google/protobuf/descriptor.proto")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("protoc: %v\n%s", err, b)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := set.UnmarshalBinary(want); err != nil {
		t.Fatal(err)
	}
	if len(set.File) < 5 {
		t.Fatalf("decoded %d files", len(set.File))
	}
	for _, f := range set.File {
		if len(f.ProtoUnknownFields()) != 0 {
			t.Errorf("%s: unexpected unknown fields", f.GetName())
		}
	}
	var sawEdition bool
	for _, f := range set.File {
		if f.GetEdition() == descriptorpb.Edition_EDITION_2023 {
			sawEdition = true
		}
	}
	if !sawEdition {
		t.Error("no edition 2023 file decoded")
	}
	got, err := set.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("re-encoded descriptor set differs (%d vs %d bytes)", len(got), len(want))
	}
}
