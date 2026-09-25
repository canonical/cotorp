// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canonical/cotorp/internal/desc"
	"github.com/canonical/cotorp/internal/gengo"
)

type testFile struct {
	name, src string
}

func writeFiles(t *testing.T, files []testFile) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// protocAccepts reports whether protoc compiles the last file, or skips
// the check if protoc is unavailable.
func protocAccepts(t *testing.T, dir, name string) (ok bool, msg string, have bool) {
	if _, err := exec.LookPath("protoc"); err != nil {
		return false, "", false
	}
	cmd := exec.Command("protoc", "-I"+dir, "-o", os.DevNull, name)
	out, err := cmd.CombinedOutput()
	return err == nil, string(out), true
}

func compile(dir, name string) error {
	_, err := generate(dir, name)
	return err
}

// generate compiles name and all other .proto files in dir, mapping each to
// its own Go package under example.com/.
func generate(dir, name string) ([]gengo.OutputFile, error) {
	c := New([]string{dir})
	f, err := c.Load(name)
	if err != nil {
		return nil, err
	}
	importMap := map[string]string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		base := strings.TrimSuffix(e.Name(), ".proto")
		importMap[e.Name()] = "example.com/" + base + ";" + base + "pb"
	}
	return gengo.New(gengo.Options{ImportMap: importMap}).Generate([]*desc.File{f})
}

func TestInvalid(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		extra []testFile
		want  string
	}{
		{"proto3 required", `syntax = "proto3"; message M { required int32 a = 1; }`, nil, "required fields are not allowed"},
		{"proto3 default", `syntax = "proto3"; message M { optional int32 a = 1 [default = 1]; }`, nil, "default values are not allowed"},
		{"proto3 enum nonzero", `syntax = "proto3"; enum E { A = 1; }`, nil, "must be zero"},
		{"duplicate number", `syntax = "proto3"; message M { int32 a = 1; int32 b = 1; }`, nil, "already been used"},
		{"field number zero", `syntax = "proto3"; message M { int32 a = 0; }`, nil, "out of range"},
		{"field number reserved range", `syntax = "proto3"; message M { int32 a = 19000; }`, nil, "reserved for the protocol buffer library"},
		{"field number too large", `syntax = "proto3"; message M { int32 a = 536870912; }`, nil, "out of range"},
		{"reserved number", `syntax = "proto3"; message M { reserved 2 to 4; int32 a = 3; }`, nil, "reserved number"},
		{"reserved name", `syntax = "proto3"; message M { reserved "a"; int32 a = 1; }`, nil, "reserved"},
		{"undefined type", `syntax = "proto3"; message M { Missing a = 1; }`, nil, "not defined"},
		{"duplicate message", `syntax = "proto3"; message M {} message M {}`, nil, "already defined"},
		{"field conflicts with nested", `syntax = "proto3"; message M { message a {} int32 a = 1; }`, nil, "already defined"},
		{"enum alias", `syntax = "proto3"; enum E { A = 0; B = 0; }`, nil, "allow_alias"},
		{"allow_alias unused", `syntax = "proto3"; enum E { option allow_alias = true; A = 0; B = 1; }`, nil, "no aliases"},
		{"enum value scoping", `syntax = "proto3"; enum E { A = 0; } enum F { A = 0; }`, nil, "C++ scoping"},
		{"map float key", `syntax = "proto3"; message M { map<float, int32> m = 1; }`, nil, "key in map"},
		{"map label", `syntax = "proto2"; message M { repeated map<int32, int32> m = 1; }`, nil, "labels are not allowed"},
		{"packed string", `syntax = "proto2"; message M { repeated string s = 1 [packed = true]; }`, nil, "packed"},
		{"proto2 missing label", `syntax = "proto2"; message M { int32 a = 1; }`, nil, "label"},
		{"editions optional", `edition = "2023"; message M { optional int32 a = 1; }`, nil, "not allowed under editions"},
		{"editions packed option", `edition = "2023"; message M { repeated int32 a = 1 [packed = true]; }`, nil, "not allowed under editions"},
		{"feature wrong target", `edition = "2023"; message M { option features.field_presence = IMPLICIT; }`, nil, "cannot be set on an entity"},
		{"feature in proto2", `syntax = "proto2"; option features.field_presence = IMPLICIT;`, nil, "only valid under editions"},
		{"features set twice", `edition = "2023"; option features.field_presence = IMPLICIT; option features = { enum_type: CLOSED };`, nil, "already set"},
		{"feature bad value", `edition = "2023"; option features.field_presence = SOMETIMES;`, nil, "invalid value"},
		{"2024 feature in 2023", `edition = "2023"; option features.enforce_naming_style = STYLE_LEGACY;`, nil, "not introduced until"},
		{"unsupported edition", `edition = "2099"; message M {}`, nil, "not supported"},
		{"implicit closed enum", `edition = "2023"; enum E { option features.enum_type = CLOSED; A = 1; } message M { E e = 1 [features.field_presence = IMPLICIT]; }`, nil, "closed"},
		{"proto3 uses proto2 enum", `syntax = "proto3"; import "p2.proto"; message M { E e = 1; }`,
			[]testFile{{"p2.proto", `syntax = "proto2"; enum E { A = 1; }`}}, "closed"},
		{"implicit message", `edition = "2023"; message M { M m = 1 [features.field_presence = IMPLICIT]; }`, nil, "implicit presence"},
		{"implicit default", `edition = "2023"; message M { int32 a = 1 [features.field_presence = IMPLICIT, default = 1]; }`, nil, "cannot specify defaults"},
		{"delimited scalar", `edition = "2023"; message M { int32 a = 1 [features.message_encoding = DELIMITED]; }`, nil, "only message fields"},
		{"empty oneof", `syntax = "proto3"; message M { oneof o {} }`, nil, "at least one field"},
		{"oneof label", `syntax = "proto2"; message M { oneof o { optional int32 a = 1; } }`, nil, "labels"},
		{"extension out of range", `syntax = "proto2"; message M { extensions 100 to 199; } extend M { optional int32 x = 5; }`, nil, "extension number"},
		{"extension overlaps field", `syntax = "proto2"; message M { optional int32 a = 5; extensions 1 to 10; }`, nil, "includes field"},
		{"scoping resolved to missing", `syntax = "proto3"; package a.b; message b { } message M { b.C x = 1; }`, nil, "is resolved to"},
		{"unknown option", `syntax = "proto3"; option java_pakage = "x";`, nil, "unknown"},
		{"duplicate option", `syntax = "proto3"; option go_package = "a"; option go_package = "b";`, nil, "already set"},
		{"import not found", `syntax = "proto3"; import "nope.proto";`, nil, "not found"},
		{"import unimported type", `syntax = "proto3"; import "a.proto"; message M { C c = 1; }`,
			[]testFile{{"a.proto", `syntax = "proto3"; import "c.proto";`}, {"c.proto", `syntax = "proto3"; message C {}`}}, "not defined"},
		{"missing semicolon", `syntax = "proto3"; message M { int32 a = 1 }`, nil, "expected"},
		{"unterminated string", `syntax = "proto3"; option go_package = "abc;`, nil, "unterminated"},
		{"bad syntax", `syntax = "proto4";`, nil, "syntax"},
		{"bad default type", `syntax = "proto2"; message M { optional int32 a = 1 [default = "x"]; }`, nil, "invalid default"},
		{"default out of range", `syntax = "proto2"; message M { optional int32 a = 1 [default = 2147483648]; }`, nil, "invalid default"},
		{"bad enum default", `syntax = "proto2"; enum E { A = 1; } message M { optional E e = 1 [default = B]; }`, nil, "no value named"},
		{"map closed enum", `syntax = "proto2"; enum E { A = 1; } message M { map<int32, E> m = 1; }`, nil, "0 as the first value"},
		{"map_entry explicit", `syntax = "proto3"; message M { option map_entry = true; }`, nil, "map_entry"},
		{"repeated default", `syntax = "proto2"; message M { repeated int32 a = 1 [default = 1]; }`, nil, "default"},
		{"json name conflict proto3", `syntax = "proto3"; message M { int32 foo_bar = 1; int32 fooBar = 2; }`, nil, "JSON name"},
		{"json name conflict editions", `edition = "2023"; message M { int32 foo_bar = 1; int32 fooBar = 2; }`, nil, "JSON name"},
		{"custom json name conflict proto2", `syntax = "proto2"; message M { optional int32 a = 1 [json_name = "x"]; optional int32 b = 2 [json_name = "x"]; }`, nil, "JSON name"},
		{"custom vs default json name", `syntax = "proto3"; message M { int32 a = 1 [json_name = "b"]; int32 b = 2; }`, nil, "JSON name"},
		{"json_name on extension", `syntax = "proto2"; message M { extensions 1 to 5; } extend M { optional int32 x = 1 [json_name = "y"]; }`, nil, "json_name is not allowed"},
		{"lowercase group", `syntax = "proto2"; message M { optional group foo = 1 {} }`, nil, "capital"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := append(tc.extra, testFile{"test.proto", tc.src})
			dir := writeFiles(t, files)
			err := compile(dir, "test.proto")
			if err == nil {
				t.Fatalf("cotorp accepted invalid input")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
			if ok, _, have := protocAccepts(t, dir, "test.proto"); have && ok {
				t.Errorf("protoc accepts this input; cotorp is stricter than protoc")
			}
		})
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		extra []testFile
	}{
		{"relative and absolute names", `syntax = "proto3"; package a.b;
			message Outer { message Inner { int32 x = 1; } Inner i = 1; .a.b.Outer.Inner j = 2; b.Outer k = 3; a.b.Outer l = 4; }
			message Other { Outer.Inner i = 1; }`, nil},
		{"shadowing", `syntax = "proto3"; package p;
			message A { message B {} } message C { message A { message B {} } A.B b = 1; .p.A.B b2 = 2; }`, nil},
		{"keywords as names", `syntax = "proto3"; package syntax.message;
			message message { int32 syntax = 1; int32 option = 2; int32 optional = 3; int32 map = 4; int32 enum = 5; int32 package = 6; int32 import = 7; int32 reserved = 8; int32 oneof = 9; int32 to = 10; int32 max = 11; }
			enum enum { syntax = 0; package = 1; }`, nil},
		{"message named map", `syntax = "proto3"; message map { int32 x = 1; } message M { map m = 1; map<string, map> mm = 2; }`, nil},
		{"ranges", `syntax = "proto2"; message M { reserved 1, 3 to 5, 100 to max; reserved "a", "b"; extensions 10 to 20, 30; optional int32 z = 6; }
			enum E { reserved -5 to -1, 100 to max; reserved "X"; A = 0; }`, nil},
		{"editions reserved identifiers", `edition = "2023"; message M { reserved a, b; int32 c = 1; } enum E { reserved X; Y = 0; }`, nil},
		{"custom options", `syntax = "proto2"; package opt; import "google/protobuf/descriptor.proto";
			message Info { optional string name = 1; repeated int32 nums = 2; optional Info child = 3; }
			extend google.protobuf.FieldOptions { optional Info info = 50000; optional int32 num = 50001; }
			extend google.protobuf.MessageOptions { repeated string tags = 50000; }
			message M {
				option (tags) = "a"; option (tags) = "b";
				optional int32 a = 1 [(info) = { name: "x" nums: [1, 2] child < name: 'y' > nums: 3 }, (num) = -5];
				optional int32 b = 2 [(opt.info).name = "z", (info).child.name = "w"];
			}`, nil},
		{"string concatenation and escapes", `syntax = "proto2"; message M { optional string s = 1 [default = "a" 'b' "\x41\101é\U0001F600\n\?"]; optional bytes b = 2 [default = "\377\0"]; }`, nil},
		{"numeric literals", `syntax = "proto2"; message M { optional int32 a = 0x10 [default = -0x7fffffff]; optional float f = 2 [default = .5]; optional double d = 3 [default = 1e10]; optional double e = 4 [default = -inf]; optional double n = 5 [default = nan]; optional uint64 u = 6 [default = 01777777777777777777777]; optional double i = 7 [default = 5]; }
			enum E { A = -0x80000000; B = 0x7fffffff; }`, nil},
		{"groups everywhere", `syntax = "proto2"; message M { optional group G = 1 { optional int32 a = 2; } repeated group R = 3 { repeated group S = 4 {} } oneof choice { group O = 5 { optional int32 x = 6; } } extensions 100 to 200; }
			extend M { optional group EG = 100 { optional int32 y = 1; } }
			message N { extend M { repeated group NG = 101 { optional int32 z = 1; } } }`, nil},
		{"services ignored", `syntax = "proto3"; message Req {} service S { rpc A(Req) returns (Req); rpc B(stream Req) returns (stream Req) { option deprecated = true; } }`, nil},
		{"comments and empty statements", `/* leading */ syntax = "proto3"; ; // trailing
			/** doc */ message M { ; // x
			  int32 a = 1; /* y */ ; } ;`, nil},
		{"public imports", `syntax = "proto3"; import "a.proto"; message M { C c = 1; }`,
			[]testFile{{"a.proto", `syntax = "proto3"; import public "c.proto";`}, {"c.proto", `syntax = "proto3"; message C {}`}}},
		{"weak import", `syntax = "proto2"; import weak "c.proto"; message M { optional C c = 1; }`,
			[]testFile{{"c.proto", `syntax = "proto2"; message C {}`}}},
		{"editions features", `edition = "2023"; import "google/protobuf/go_features.proto"; option features = { field_presence: IMPLICIT enum_type: CLOSED };
			enum E { option features.enum_type = OPEN; option features.(pb.go).legacy_unmarshal_json_enum = false; A = 0; }
			message M { int32 a = 1; int32 b = 2 [features = { field_presence: EXPLICIT }]; repeated int32 c = 3 [features.repeated_field_encoding = EXPANDED];
			  M d = 4 [features.message_encoding = DELIMITED]; oneof o { int32 e = 5; } }`, nil},
		{"edition 2024", `edition = "2024"; import option "google/protobuf/descriptor.proto"; option features.enforce_naming_style = STYLE_LEGACY;
			export message M { local enum E { A = 0; } E e = 1; }`, nil},
		{"proto3 optional", `syntax = "proto3"; message M { optional int32 a = 1; optional M m = 2; }`, nil},
		{"json_name and deprecated", `syntax = "proto3"; message M { int32 a = 1 [json_name = "b", deprecated = true]; } enum E { option deprecated = true; A = 0 [deprecated = true]; }`, nil},
		{"json name default conflict proto2", `syntax = "proto2"; message M { optional int32 foo_bar = 1; optional int32 fooBar = 2; }`, nil},
		{"json name legacy editions", `edition = "2023"; message M { option features.json_format = LEGACY_BEST_EFFORT; int32 foo_bar = 1; int32 fooBar = 2; }`, nil},
		{"json name case differs", `syntax = "proto3"; message M { int32 foo = 1; int32 Foo = 2; }`, nil},
		{"message set", `syntax = "proto2"; message MS { option message_set_wire_format = true; extensions 4 to max; } message X { extend MS { optional X ext = 2147483646; } }`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := append(tc.extra, testFile{"test.proto", tc.src})
			dir := writeFiles(t, files)
			if ok, msg, have := protocAccepts(t, dir, "test.proto"); have && !ok {
				t.Fatalf("test case is invalid according to protoc:\n%s", msg)
			}
			outs, err := generate(dir, "test.proto")
			if err != nil {
				t.Fatalf("cotorp rejected valid input: %v", err)
			}
			if testing.Short() {
				return
			}
			// Type-check the generated code (dependencies are generated as
			// separate packages alongside).
			build := t.TempDir()
			all := []gengo.OutputFile{}
			for _, f := range files {
				o, err := generate(dir, f.name)
				if err != nil {
					t.Fatalf("generating %s: %v", f.name, err)
				}
				all = append(all, o...)
			}
			_ = outs
			for _, o := range all {
				p := filepath.Join(build, filepath.FromSlash(strings.TrimPrefix(o.Name, "example.com/")))
				os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, o.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			os.WriteFile(filepath.Join(build, "go.mod"), []byte("module example.com\n\ngo 1.22\n"), 0o644)
			cmd := exec.Command("go", "vet", "./...")
			cmd.Dir = build
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated code does not build: %v\n%s", err, out)
			}
		})
	}
}
