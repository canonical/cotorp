// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package testprotos holds generated test packages and wire-compatibility
// tests that cross-check the generated code against protoc.
package testprotos

//go:generate go run ../../cmd/cotorp -I proto -go_out . -module github.com/canonical/cotorp/internal/testprotos -M google/protobuf/descriptor.proto=github.com/canonical/cotorp/internal/testprotos/descriptorpb common/common.proto proto3/scalars.proto proto2/proto2.proto editions/editions.proto editions/editions2024.proto google/protobuf/descriptor.proto
