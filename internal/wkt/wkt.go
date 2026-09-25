// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package wkt embeds the standard google/protobuf/*.proto files so that
// imports such as "google/protobuf/timestamp.proto" resolve without an
// external include directory.
//
// The embedded files are copied unmodified from the protobuf distribution
// and are covered by its BSD-3-Clause license (see LICENSE).
package wkt

import "embed"

//go:embed google
var FS embed.FS
