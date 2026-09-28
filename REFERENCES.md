# References

This file lists the outside knowledge used to build cotorp. It separates
material that was actually opened or run during development from material
that came from prior familiarity and was **not** re-read. Anything in the
second group has only been checked by the test suite, not against its source.

## 1. Sources read or run during development

### protobuf distribution (protoc 36.1, `libprotoc 36.1`)

Installed at `/nix/store/4hdzvycn5mkg3xm1ggscp3mxz8005c3m-protobuf-36.1`.

- **`include/google/protobuf/*.proto`**, copied unmodified into
  `internal/wkt/google/` (BSD-3-Clause, see `internal/wkt/LICENSE`): `any`,
  `api`, `compiler/plugin`, `cpp_features`, `cpp_file_options`,
  `cpp_options`, `c_sharp_features`, `descriptor`, `duration`, `empty`,
  `field_mask`, `go_features`, `java_features`, `json_enumvalue_options`,
  `json_options`, `source_context`, `struct`, `timestamp`, `type`,
  `wrappers`.
- **`descriptor.proto`**, read directly for:
  - the file header (`syntax = "proto2"`, `go_package`), which showed that
    the bundled well-known types point at `google.golang.org/protobuf/...`
    and need `-M` remapping;
  - the `Edition` enum values (`EDITION_PROTO2 = 998`, `EDITION_PROTO3 = 999`,
    `EDITION_2023 = 1000`, `EDITION_2024 = 1001`), used in `internal/desc`;
  - the `FeatureSet` message: every feature's value names, its `targets`
    (e.g. `field_presence` may only be set on FIELD and FILE), and its
    `edition_introduced`. `featureInfos` in `internal/desc/options.go` is
    transcribed from this.
  - the fields of `FileOptions`, `MessageOptions`, `FieldOptions`,
    `OneofOptions`, `EnumOptions` and `EnumValueOptions`, used for the
    known-option lists in `internal/desc/options.go`, including which are
    `repeated` (`targets`, `edition_defaults`, `uninterpreted_option`).
- **The `protoc` binary**, used as the reference implementation:
  - `protoc --encode`, `--decode`, `--decode_raw` and `--descriptor_set_out`
    are the oracle for all wire-format tests in `internal/testprotos`.
  - `protoc -o /dev/null` decides whether each schema in
    `internal/compiler/compiler_test.go` is valid.
  - Behaviours found by experiment and then copied into cotorp:
    - "Enum value in map must define 0 as the first value" (closed enums as
      map values);
    - "Option ... cannot be set on an entity of type `message`" (feature
      targets);
    - "Option "features" was already set" (aggregate plus sub-field feature
      assignment);
    - a file-level `message_encoding = DELIMITED` does not apply to map
      fields or their values: `protoc --encode` produced a length-prefixed
      entry (`1a 06 08 01 12 02 08 02`) next to a group-encoded singular
      field (`23 08 03 24`);
    - an explicit `message_encoding` on a map or non-message field is
      rejected ("Only message fields can specify message encoding");
    - `pb.GoFeatures.legacy_unmarshal_json_enum` may only be set on enums,
      and custom feature extensions need their defining file imported;
    - in an enum body, `option` is always treated as a keyword;
    - enum values in a package clash with sibling messages of the same name
      (C++ scoping), with protoc's note on this rule.

### protobuf-go module (`google.golang.org/protobuf@v1.36.11`, local module cache)

- **All 130 `.proto` files** in the module, used as a parser corpus
  (`COTORP_CORPUS` in `internal/parser/corpus_test.go`) and as a code
  generation corpus that must build and vet at Go 1.27. The corpus run is a
  manual script, not part of `go test`.
- **`src/google/protobuf/go_features.proto`**: its location was checked;
  cotorp bundles the protoc distribution's copy instead.
- **`cmd/protoc-gen-go/testdata/protoeditions/maps_and_delimited.proto`**:
  read after cotorp wrongly rejected it. It showed that inherited DELIMITED
  must be allowed on map fields.
- **`cmd/protoc-gen-go/testdata/extensions/base/base.proto`** and
  **`internal/testprotos/messageset/...`**: their failures showed that
  cotorp must accept `message_set_wire_format` and treat MessageSet `max`
  as 2^31−1.
- `internal/impl/codec_map.go` was grepped for DELIMITED handling. It had
  no matches, and nothing from it was used.

### protobuf-go `protojson` (JSON reference implementation)

protoc has no JSON mode. For JSON support, a throwaway program in the
development scratch directory (not in the repository) was built offline
against the cached `google.golang.org/protobuf@v1.36.11`. It loads protoc's
descriptor sets through `protodesc`, `dynamicpb` and `protojson`, and converts
between binary and ProtoJSON. Every case in `internal/testprotos/json_test.go`
was checked against it in both directions. Behaviours taken from it:

- a top-level `null` is rejected (cotorp deliberately differs; see README);
- duplicate map keys are rejected (`encoding/protojson/decode.go`, read in
  the cached v1.36.8 source);
- strings are escaped as `\"`, `\\`, `\b`, `\f`, `\n`, `\r`, `\t` and
  lowercase `\u00xx` for other control characters, with no HTML escaping, and
  floats use `encoding/json`'s format (`appendString` and `appendFloat` in
  `internal/encoding/json/encode.go`, v1.36.8). `jsontext.AppendQuote` and
  `jsontext.AppendFloat` produce the same bytes (`TestJSONMarshalExact`);
- unknown numbers for closed enums are accepted from JSON;
- an implicit `NullValue` field holding an undeclared number encodes as
  `null`.

### Go `encoding/json/v2` and `jsontext` (Go 1.27, found by experiment)

- The `jsonv2` experiment is on by default in Go 1.27
  (`internal/buildcfg/exp.go`), so generated code may import
  `encoding/json/jsontext` and `encoding/json/v2` without `GOEXPERIMENT`.
- `Decoder` rejects invalid UTF-8 and duplicate object names by default.
  `Token` and `Value` results are invalidated by the next read or peek.
- `NewDecoder` reads a `*bytes.Buffer` in place, without copying, and does
  not modify it.
- `encoding/json` (v1) calls `UnmarshalJSONFrom` in preference to
  `UnmarshalJSON`, with `AllowInvalidUTF8(true)`, which replaces invalid
  UTF-8 in strings; `json.GetOption` reports the setting
  (`encoding/json/v2_decode.go`, `v2/arshal_methods.go`).

### protoc JSON-name validation (found by experiment)

- With `json_format = ALLOW` (the proto3 and editions default), any JSON-name
  conflict is an error.
- With `LEGACY_BEST_EFFORT` (the proto2 default), conflicts between two
  default names are only warnings, but conflicts involving a custom
  `json_name` are errors.
- Names are compared case-sensitively (`foo` and `Foo` do not conflict).
- `json_name` is rejected on extensions.

### Go toolchain (go1.27.1)

- `go vet`, `go test -fuzz`, `go/format`, `go/parser` and `go/ast` were used
  as tools. Nothing was taken from their source.
- Compiler behaviour found with `go build -gcflags=-m` and allocation
  profiles, which shaped the per-file helpers (`internal/gengo/helpers.go`):
  - a method that passes its receiver to a generic function calling the
    receiver's methods makes the receiver escape (`moved to heap`), because
    pointer type arguments share one instantiation;
  - a varint writer with a one-byte fast path inlines (cost 67 of 80), but a
    length-delimited reader does not (cost 103 to 105);
  - a small `make` with a variable length, or a `string([]byte)` conversion
    of up to 32 bytes, stays on the stack when it does not escape, but not
    once a helper returns it.

## 2. Prior knowledge used without re-reading its source

These came from familiarity with the ecosystem. No source was opened during
this work. Correctness relies on the protoc-based tests.

### Protocol Buffers

- **Language specs** for proto2, proto3 and editions
  (https://protobuf.dev/reference/protobuf/proto2-spec/, `proto3-spec/`,
  `edition-2023-spec/`): grammar, labels, groups, maps, oneofs,
  reserved/extension ranges, `max`, and literal and escape syntax.
- **Encoding guide** (https://protobuf.dev/programming-guides/encoding/):
  - wire types, tag layout, varints, zigzag, fixed widths, packed repeated
    fields, groups, map entries encoded as key=1/value=2 messages;
  - "last one wins" for scalars and merging for repeated occurrences of
    message fields;
  - field numbers 19000–19999 are reserved and the maximum is 2^29−1.
- **Editions overview and feature defaults**
  (https://protobuf.dev/editions/features/): per-edition defaults for
  `field_presence`, `enum_type`, `repeated_field_encoding`,
  `utf8_validation` and `message_encoding`, and how proto2/proto3 map onto
  them (required → LEGACY_REQUIRED, group → DELIMITED, `[packed]`,
  proto3 `optional` → EXPLICIT).
- **Enum behaviour** (https://protobuf.dev/programming-guides/enum/): closed
  enums send unrecognised values to the unknown fields, open enums keep
  them.
- **protoc name resolution**: search from the innermost scope outward, and
  the "is resolved to ..., which is not defined" rule for qualified names.
  The error text is paraphrased from memory.
- **protoc's map entry naming** (`foo_bar` → `FooBarEntry`) and the rule
  that protoc serializes fields in field-number order with unknown fields
  last.
- **C++ `TextFormat` prints map entries sorted by key.** The map test
  harness relies on this.
- **ProtoJSON mapping** (https://protobuf.dev/programming-guides/json/):
  - JSON names and the proto-name fallback on input;
  - 64-bit integers as strings, and integers accepted as strings or exact
    exponent forms;
  - NaN and Infinity as strings;
  - base64 alphabets;
  - null semantics, including `Value` and `NullValue`;
  - the well-known-type formats, including Timestamp and Duration ranges and
    0/3/6/9 fractional digits, and FieldMask camelCase conversion.

  The details were written from memory; `protojson` confirmed them (above).
- **protoc's `ToJsonName`** (drop `_` and upper-case the next character).

### protobuf-go

- **The `GoCamelCase` algorithm** (`internal/strs`), reimplemented from
  memory in `internal/gengo/names.go`.
- **protoc-gen-go naming conventions**:
  - `Outer_Inner` for nested types;
  - `Enum_VALUE` / `Parent_VALUE` for enum constants, and the `Enum_name` /
    `Enum_value` maps;
  - `Msg_Field` oneof wrappers and `isMsg_Oneof` interfaces;
  - `Default_Msg_Field` constants;
  - appending `_` to fields that clash with methods or getters;
  - the `M` flag and `paths=import|source_relative`, `module=` options;
  - `go_package` `path;name` syntax.
- **`protowire` semantics**:
  - a varint is rejected if its 10th byte is greater than 1;
  - field number 0 is invalid;
  - the default recursion limit is 10,000;
  - a known field with an unexpected wire type is treated as unknown;
  - closed-enum values inside packed fields are split into individual
    unknown varints;
  - proto3 strings are UTF-8 validated on both marshal and unmarshal.

### Other Go ecosystem conventions

- **gogo/protobuf `MarshalToSizedBuffer`**: writing messages back to front
  so length prefixes are known in one pass. cotorp's
  `ProtoMarshalToSizedBuffer` takes its name and approach from it.
- **Go standard library interfaces**: `encoding.BinaryMarshaler` /
  `BinaryUnmarshaler`, and `encoding.BinaryAppender` (Go 1.24).
- **Go language rules** the generated code depends on:
  - `goto` may not jump into a block or over variable declarations;
  - unused labels, imports and local variables are compile errors;
  - Go constants have no negative zero, so `-0.0` defaults use
    `math.Copysign`;
  - `new(expr)` (Go 1.26) allocates a copy of a value, which generated code
    uses to set explicit-presence fields;
  - in an assignment such as `b, m.F = b[n:], conv(b)`, every operand on the
    right is evaluated before anything is assigned, so both see the old `b`;
  - gofmt keeps a one-statement function body on one line, but expands a
    parenthesized `var (...)` group onto several lines.
- **The varint size formula** `(bits.Len64(v|1)+7-1)/7`: derived, and checked
  by the protoc byte-comparison tests.
