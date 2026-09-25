# AGENTS.md

Guidance for AI coding agents working on cotorp. See README.md for user-facing
documentation.

## What this project is

cotorp is a protobuf compiler that generates Go marshal/unmarshal code. It has
two hard requirements:

1. **Generated code imports only the Go standard library** (plus other
   cotorp-generated packages it references). It has no runtime package, no
   shared helper file, and no protobuf-go dependency.
2. **The compiler itself is stdlib-only.** It has its own `.proto` parser and
   does not use protoc or any third-party module. This includes tests: never
   add a dependency to `go.mod`.

Only messages and enums are generated. gRPC services are parsed and then
ignored.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/cotorp` | CLI: flags, input resolution, writing output files. |
| `internal/parser` | Lexer, recursive-descent parser and AST for proto2, proto3 and editions. |
| `internal/desc` | Linker: symbol tables, protoc-style name resolution, lowering of every syntax to per-field editions features, and validation. |
| `internal/gengo` | Go generator. `gengo.go` handles naming, packages and imports; `message.go` handles types and getters; `methods.go` handles size, marshal, unmarshal and required checks; `kinds.go` holds per-kind snippets. |
| `internal/compiler` | Loads files from `-I` paths plus the bundled includes, and links imports in dependency order. |
| `internal/wkt` | Embedded `google/protobuf/*.proto` files, copied unmodified from protoc 36.1. Do not edit them; they are under Google's license (`internal/wkt/LICENSE`). |
| `internal/testprotos` | Test `.proto` sources (`proto/`), the committed generated packages, and the compatibility, fuzz and staleness tests. |

## Commands

```sh
go test ./...                         # full suite; needs protoc 36.1 on PATH for the cross-checks
go generate ./internal/testprotos     # regenerate committed test packages
go vet ./... && gofmt -l .            # must be clean (CI enforces both)
go test ./internal/testprotos -run '^$' -fuzz FuzzRoundTrip -fuzztime 60s
```

Without protoc on PATH, the protoc-based tests skip, so a passing local run
proves little. CI installs protoc 36.1 and fails if it is missing
(`.github/workflows/test.yml`).

## Rules for changing the generator

- **After any change to `internal/gengo` or `internal/desc`, run
  `go generate ./internal/testprotos` and commit the regenerated `*.pb.go`
  files.** `TestGeneratedUpToDate` fails if they are stale.
- **Generated code must be self-contained.** Every file declares types,
  constants, vars and methods only, never package-level functions.
  `TestSelfContained` enforces this. All wire logic is inlined into methods;
  methods may call the standard library and the methods of other generated
  messages (`ProtoSize`, `ProtoMarshalToSizedBuffer`, `ProtoMergeDepth`,
  `ProtoCheckInitialized`, `IsValid`).
- **Stdlib imports are tracked.** Reference a stdlib package only through
  `fg.std("import/path")`, which records the import and returns the package
  name. Writing `binary.` or `math.` directly into generated code leads to a
  missing-import or unused-import compile error. Use `fg.errExpr(msg)` for
  errors.
- **Adding a local variable name** to generated method bodies means adding it
  to `localNames` in `gengo.go`, so that import aliases cannot shadow it.
- **`goto` rules in `ProtoMergeDepth`:**
  - Decode failures `goto errParse` and depth failures `goto errDepth`; both
    labels sit at the end of the function.
  - Declare no variables at the top level of the function body between the
    first `goto` and the labels. Declare them inside the `for` loop or other
    blocks, or the jump will not compile.
  - Every label must be used.
- **`ProtoMarshalToSizedBuffer` writes back to front.** Fields are emitted in
  descending field-number order so that the output ends up ascending.
  - Unknown fields are copied first, so they end up last.
  - `fg.encVarint` uses the scratch variable `u`; the declaration is emitted
    only when `fg.usesU` is set during `fg.capture`.
- **Wire output must stay byte-identical to protoc** for messages without
  maps. Map entries are sorted by key.
- **Generated code must build at Go 1.21.** Do not use newer language or
  library features in emitted code (for example `new(expr)` or range over int).
  Tests may use anything supported by the `go.mod` version.
- **Naming follows protoc-gen-go** (`camelCase` in `names.go`, `_` suffixes
  for conflicts, `Msg_Field` oneof wrappers). Changing a name is a breaking
  change for users.

## Rules for changing the parser or linker

- protoc is the reference. When accepting or rejecting a construct, match
  protoc 36.1: do not be stricter, and prefer not to be looser.
- **Every new validation needs cross-checked test cases:**
  - Add rejected inputs to `TestInvalid` and accepted inputs to `TestValid`
    in `internal/compiler/compiler_test.go`.
  - Both tests run protoc on each case. `TestInvalid` fails if protoc accepts
    the input; `TestValid` fails if protoc rejects the test input itself.
- Feature definitions (values, targets, `edition_introduced`) and known option
  names are transcribed from `internal/wkt/google/protobuf/descriptor.proto`.
  Update them from that file, not from memory.
- Supported editions are `2023` and `2024`. Adding one means updating the
  `Edition` constants, `defaultFeatures`, `featureInfos` and the edition switch
  in `build.go`.

## Testing conventions

- **Wire-format behaviour:** add a case to `TestProto3Compat`,
  `TestProto2Compat` or `TestEditionsCompat` in
  `internal/testprotos/compat_test.go`. Each case gives a Go value and the
  equivalent protoc text format. The harness checks:
  - that marshal output matches protoc's encoding;
  - that unmarshaling protoc's encoding yields the Go value;
  - that the result re-encodes stably.

  Set `unordered` for messages containing maps.
- **New schema features:** add them to the protos in
  `internal/testprotos/proto/`, and to the `go:generate` line in
  `internal/testprotos/gen.go` if you add a new file.
- **Decoder changes:** run the fuzzer, and add a regression input to
  `TestMalformed` or `TestRecursionLimit` where relevant.

## Housekeeping

- Every non-generated `.go` file starts with:

  ```go
  // Copyright (C) 2026 by Canonical Ltd
  // Licensed under BSD-3-Clause, see LICENSE file for details.
  ```

  followed by a blank line. Generated `*.pb.go` files carry only the
  `// Code generated by cotorp. DO NOT EDIT.` header.
- Record new outside sources you rely on (specs, protoc behaviour found by
  experiment, upstream code) in REFERENCES.md.
- Known gaps are listed in README.md:
  - no typed extension accessors (extensions round-trip as unknown fields);
  - no JSON, text format or reflection;
  - custom options are parsed but not validated.
