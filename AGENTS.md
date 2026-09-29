# AGENTS.md

Guidance for AI coding agents working on cotorp. See README.md for user-facing
documentation.

## What this project is

cotorp is a protobuf compiler that generates Go marshal/unmarshal code. It has
two hard requirements:

1. **Generated code imports only the Go standard library** (plus other
   cotorp-generated packages it references). It has no runtime package and
   no protobuf-go dependency. Each file declares the helper functions it
   uses, unless `-helpers` names a package that cotorp generates to hold
   them all, exported, for every file to import.
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
| `internal/gengo` | Go generator. `gengo.go` handles naming, packages and imports; `message.go` handles types and getters; `methods.go` handles binary size, marshal, unmarshal and required checks; `json.go` handles ProtoJSON, including the well-known types; `kinds.go` holds per-kind snippets; `consts.go` holds the file-level constants; `helpers.go` holds the per-file helper functions. |
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
- **Generated code must be self-contained.** A file never depends on another
  file's code other than the exported API of generated messages
  (`ProtoSize`, `ProtoMarshalToSizedBuffer`, `ProtoMergeDepth`,
  `ProtoCheckInitialized`, `ProtoAppendJSON`, `ProtoMergeJSONFrom`) and
  enums (`IsValid`, `E_name`, `E_value`).
  - Code that would repeat in every message goes in a helper function,
    declared from `helperDefs` in `helpers.go` and named `fg.fn("Name")`:
    the file prefix plus the suffix, declared in the file, or
    `<alias>.Name` in the shared package generated with `Options.Helpers`
    (`generateHelpers`, which emits every helper and constant, exported,
    with its `doc`; give each new helper one). Constants (`fg.c`) and the
    error messages helpers use (`errString`, through `g.sharedErrs`) follow
    the same rule. A file declares only the helpers it uses.
    `TestSelfContained` rejects package-level functions without the file's
    prefix, except in a shared package, whose header says `// helpers:`.
    `gen.go` generates `otlp` with `-helpers` and the other packages
    without, so both forms are tested.
  - Names that stay per file are short, since they are unexported: the
    prefix plus `E<n>` for error message constants (numbered in order of
    first use), `Z<Msg>` for zero values, `J<Enum>` for the JSON parse
    function of an enum, and the struct field `u` for unknown fields
    (`unknownField` in `message.go`).
  - **Helpers never take a message.** Calling methods through an interface
    or a type parameter makes the message escape to the heap, so
    `var m M; m.UnmarshalBinary(b)` would allocate. A method calls its own
    methods and passes the results, or passes a method value of itself
    (`AppendBinary`, `MarshalTo`, `MergeJSON`, `MergeFrom`) or a function
    literal calling a sub-message's method (`SizeMsg`, `AppendMsg`); with
    the helper inlined, the compiler calls the method directly and nothing
    escapes. A method expression such as `(*Msg).ProtoSize` stays an
    indirect call. Check with `go build -gcflags=-m`.
  - The JSON field helpers (`Field`, `RepField`, ...) call the parse
    function through a parameter, which makes the receiver of
    `ProtoMergeJSONFrom` escape: a message decoded from JSON on the stack
    costs one allocation. Binary decoding keeps direct calls.
  - Keep hot-path helpers inlinable or measure them. The inlining budget is
    80: a generic helper's wrapper adds about 9, so the field writers are
    plain functions per kind; `binary.PutUvarint` costs 57 as a call, so
    the writers loop instead; `If` lets one-line helpers avoid `if`, but
    both alternatives are evaluated, so use it only where that is cheap and
    safe (never around `*p`, an allocation or `errors.New`). `cmp.Or`
    builds a slice and doubled getter time; use `If`. A slice or string a
    helper returns is heap-allocated, where the same code inline may use the
    stack: `SortedKeys` takes a slice the caller makes, and enum names are
    looked up from the token in `JSONEnum` rather than returned.
- **Stdlib imports are tracked.** Reference a stdlib package only through
  `fg.std("import/path")`, which records the import and returns the package
  name. Writing `binary.` or `math.` directly into generated code leads to a
  missing-import compile error. An expression built but not emitted still
  records its import; `dropUnusedImports` removes imports the finished file
  does not reference, so never name a local after a package and use it with
  a selector. `otlp/ids.proto` (hex bytes fields only, so no base64) guards
  this.
- **No magic numbers or repeated error strings in generated code.**
  - Numbers with a fixed meaning (wire types, sizes, limits)
    are written as `fg.c("Name")`, which declares the constant from
    `constGroups` in `consts.go` at the top of the file. Add new ones there.
    Schema data (field numbers, enum values, tag sizes) stays literal.
  - Errors are built with `fg.errExpr(hint, msg)` or
    `fg.errConcat(hint, msg, parts...)`. A message used more than once in a
    file becomes a constant named by `hint` (`"ErrParse"`, or
    `fg.owner(fullName)+"ErrX"` for per-message text); otherwise it stays an
    inline literal. Inside helpers, JSON errors about a message are built
    with `fg.jsonErrorf`, which calls the `JSONError` helper with the
    message name passed in.
  - Constant and helper names are the file's prefix (the lowerCamel `.proto` base
    name) plus a suffix, because several files may share a Go package.
    Names are deduplicated against the package within one run.
- **No bare `{ }` blocks.** Emitted snippets must not need their own scope;
  they already sit in an `if`, `for` or `case` body.
- **Adding a local variable name** to generated method bodies means adding it
  to `localNames` in `gengo.go`, so that import aliases cannot shadow it.
- **`ProtoMergeDepth` switches on the whole tag.** Each field contributes
  `case num<<TagTypeBits | WireX:` cases (two for packable repeated fields);
  the `default` case is the `Unknown` helper, which skips the field with
  `SkipField` (the only check of the field number) and keeps it with a
  canonical tag. Map entries decode with the same pattern, inline.
  - Each case is one call of a decoder (`Varint`, `Bytes`, `String`, the
    `Packed*` and `ClosedEnum` helpers, ...) writing through a pointer:
    `&m.F`, `New(&m.F)` for explicit presence or `Grow(&m.F)` for a
    repeated element. It returns the length consumed, zero or negative if
    malformed, and `String` also an error. Messages take two lines:
    `ReadBytes` into `x`, then `ProtoMergeDepth` on `Alloc(...)`.
  - The check comes once, after the switch: `n <= 0 || err != nil`. The
    loop condition also stops on `err`, which `Depth` sets before the
    first iteration, and `End(b, err)` reports leftover bytes. There is no
    `goto`.
- **`ProtoMarshalToSizedBuffer` writes back to front.** Fields are emitted in
  descending field-number order so that the output ends up ascending.
  - Unknown fields are copied first, so they end up last.
  - Implicit-presence scalars are written by one call of a `Put*Field`
    helper, which skips zero values and writes a one-byte tag (or a
    two-byte one with the `2` variants, except for strings and bytes, whose
    writer would not inline). Other values are written under their
    presence check by one expression that evaluates to the new `i`:
    `fg.putScalar` returns it, and the tag write wraps it:
    `i = PutVarint(b, <value>, <tag>)`.
  - Singular strings are validated first, in one `CheckUTF8` chain, so the
    writes need no check; repeated and map strings are checked in their
    loops.
- **Keep generated code dense.** Where gofmt allows it, prefer one line:
  - one-statement methods on one line (`MarshalBinary`, `ProtoMerge`, and
    `ProtoCheckInitialized` and `MarshalJSON` without required fields);
  - `*m = M{}; return ...` and the like: gofmt keeps several statements on
    one line when the whole function fits in 100 characters, and splits
    it otherwise, so emit such bodies on one line;
  - `a, b := x, y` for several locals (gofmt expands `var (...)` groups);
  - `cmp.Or(m.ProtoMergeDepth(b, 0), m.ProtoCheckInitialized())` and the
    like, where a method returns the first of two errors (not on hot
    paths, see `If` above);
  - `Alloc(&m.F)` to allocate a sub-message on first use;
  - a nil receiver is replaced by the message's zero value, declared in
    one `var` block per file: getters read `m.z().F` (`Deref` and
    `BytesOr` apply defaults), and `ProtoSize`, marshaling and
    `ProtoAppendJSON` start with `m = m.z()` instead of a nil check.
    `ProtoCheckInitialized` keeps its nil check, because unset
    sub-messages pass through it. Oneof member getters keep their type
    assertion: a generic helper asserts through a dictionary and was 60%
    slower;
  - lookup tables built at init time where only startup pays
    (`E_value = Invert(E_name)` for enums without aliases). JSON field
    names stay in a `switch`: a map lookup was four times slower;
  - one-line doc comments on exported methods, and helpers without blank
    lines between them (gofmt requires one only before a documented
    declaration or a change of kind);
  - no blank lines between declarations that gofmt lets touch (getters, and
    the oneof types, then their marker methods).
- **Emit comments only on exported declarations.** Helpers, file constants,
  zero values and the per-enum JSON functions have none, and no comments
  are emitted inside function bodies. Explain generated code with comments in the generator instead
  (`constGroups` keeps its docs for this reason).
- **Measure density changes that touch hot paths.** Some are not free: an
  unconditional `copy` of empty unknown fields cost 4% of binary marshaling,
  so it stays guarded.
- **Wire output must stay byte-identical to protoc** for messages without
  maps. Map entries are sorted by key.
- **Generated code must build at Go 1.27**, the `go.mod` version. It uses
  `slices.Backward` for the back-to-front marshal loops.
- **Naming follows protoc-gen-go** (`camelCase` in `names.go`, `_` suffixes
  for conflicts, `Msg_Field` oneof wrappers). Changing a name is a breaking
  change for users.
- **JSON uses `encoding/json/jsontext`** (see the comment at the top of
  `json.go`). Marshal appends with `jsontext.AppendQuote` and
  `jsontext.AppendFloat`; `ProtoAppendJSON` declares `err` only when
  `fg.usesErr` is set.
  - Each member is appended with a leading comma (`,"name":value`), and the
    `CloseObject` helper turns the first comma into `{`.
  - `fg.jsonOutExpr` returns a scalar value as one expression appended to a
    literal (the key, and the opening quote of 64-bit integers and bytes);
    `fg.jsonOut` emits it, or the `Quote` or `AppendMsg` call for strings
    and messages. Repeated fields and maps are one `AppendList` or
    `AppendMap` call with function literals for the elements. These
    helpers take `err` and do nothing once it is set, so that the method
    returns the first error at the end without checks; strings are not
    validated twice (that cost 9%).
- **JSON unmarshal streams from one decoder.** `ProtoMergeJSONFrom` loops
  with `OpenObject` and `NextKey`, which return the next key token, or
  `EndObject` at the closing brace and for null; an error comes back as an
  invalid token and the body returns it. The token, not a string, keeps
  the key on the stack. Each key's case is one call of a field helper
  (`Field`, `OptField`, `RepField`, `MapField` or `OneofField`) with the
  destination and the parse function for the field's type: `JSONInt`,
  `JSONString`, ... for scalars, `JSONMsg` for messages, and a per-enum
  function declared after the message (`enumJSONFunc`) for enums, so that
  every parse function has the same signature. The helpers reject
  duplicate keys and handle null; the scalar parsers leave the
  destination unchanged on null, and `JSONEnum` and `JSONMsgNull` treat
  it as a value for `NullValue` and `Value`. Nested messages call
  `ProtoMergeJSONFrom` on the same decoder.
  - A `jsontext.Token` is invalid after the next read or peek, so convert it
    (for example with `tok.String()`) before using the decoder again.
  - The integer and float parsers are generic over the field's Go type and
    derive the bit width from the type (`T(1)<<31 < 0`), rather than take
    it as a parameter, to keep that signature.
  - Build error messages with dynamic parts using `fg.errConcat`.
  - Well-known types are selected by full name in `genJSON`.
  - The `-json_*` flags arrive as `gengo.Options` (`JSONEnumNumbers`, `JSONHex`,
    `JSONDiscardUnknown`) and are read at generation time. Generated code has
    no runtime options.
    - `JSONHex` names are validated in `checkJSONHex`.
    - Hex fields parse with `JSONHex` instead of `JSONBytes`.
    - `internal/testprotos/otlp` is generated with all three flags by the
      second `go:generate` line in `gen.go`, and is tested by `otlp_test.go`.

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
- **JSON:** add encoding cases to `TestJSONMarshal` and parsing cases (valid
  and invalid) to `TestJSONUnmarshal` in `internal/testprotos/json_test.go`.
  - protoc has no JSON mode, so the reference is protobuf-go's `protojson`.
    Set `COTORP_JSON_ORACLE` to a program invoked as
    `oracle <descriptor set> <message full name> tojson|fromjson`. It reads
    binary or JSON on stdin and writes the other form using
    `protodesc` + `dynamicpb` + `protojson`.
  - The oracle must live outside this repository, because the repository
    stays stdlib-only.
  - Every case must agree with the oracle. A deliberate difference goes in
    `jsonDeviations` with a reason.
  - `FuzzJSON` fuzzes the JSON decoder.

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
  - no text format or reflection, and no JSON for `google.protobuf.Any`;
  - custom options are parsed but not validated.
