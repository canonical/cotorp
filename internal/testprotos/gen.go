// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package testprotos holds generated test packages and wire-compatibility
// tests that cross-check the generated code against protoc.
package testprotos

//go:generate go run ../../cmd/cotorp -I proto -go_out . -module github.com/canonical/cotorp/internal/testprotos -M google/protobuf/descriptor.proto=github.com/canonical/cotorp/internal/testprotos/descriptorpb -M google/protobuf/any.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb -M google/protobuf/duration.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb -M google/protobuf/empty.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb -M google/protobuf/field_mask.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb -M google/protobuf/struct.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb -M google/protobuf/timestamp.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb -M google/protobuf/wrappers.proto=github.com/canonical/cotorp/internal/testprotos/wkt;wktpb common/common.proto proto3/scalars.proto proto2/proto2.proto editions/editions.proto editions/editions2024.proto jsontest/json.proto google/protobuf/descriptor.proto google/protobuf/any.proto google/protobuf/duration.proto google/protobuf/empty.proto google/protobuf/field_mask.proto google/protobuf/struct.proto google/protobuf/timestamp.proto google/protobuf/wrappers.proto
