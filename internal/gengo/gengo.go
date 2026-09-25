// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package gengo generates Go code for protobuf messages. The generated code
// implements binary marshaling and unmarshaling and imports nothing outside
// the Go standard library (other than other generated packages). All wire
// format logic is inlined into each message's methods, so generated files
// share no helper code.
package gengo

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"sort"
	"strings"

	"github.com/canonical/cotorp/internal/desc"
)

// Options configures code generation.
type Options struct {
	// Paths is "import" (default) to place output files by Go import path,
	// or "source_relative" to place them next to the .proto path.
	Paths string
	// Module is a Go module path prefix stripped from import-mode paths.
	Module string
	// ImportMap maps .proto file paths to Go import paths, overriding
	// go_package (like protoc-gen-go's M flags). A value may be of the form
	// "import/path;name".
	ImportMap map[string]string

	// JSONEnumNumbers writes enum values as numbers instead of names in
	// ProtoJSON output. Decoding accepts both regardless.
	JSONEnumNumbers bool
	// JSONHex lists fully-qualified names of bytes fields (for example
	// "pkg.Msg.trace_id") whose ProtoJSON form is a hex string instead of
	// base64. Output is lowercase; decoding accepts either case.
	JSONHex []string
	// JSONDiscardUnknown makes ProtoJSON decoding skip unrecognized keys
	// instead of rejecting them.
	JSONDiscardUnknown bool
}

// OutputFile is a generated file.
type OutputFile struct {
	Name    string // slash-separated path relative to the output directory
	Content []byte
}

// Generator holds naming state shared across files.
type Generator struct {
	opts     Options
	jsonHex  map[string]bool
	pkgs     map[*desc.File]goPackage
	msgNames map[*desc.Message]string
	enmNames map[*desc.Enum]string
	valNames map[*desc.EnumValue]string
	msgInfos map[*desc.Message]*messageInfo
	// pkgIdents records package-level identifiers per Go import path.
	pkgIdents map[string]map[string]bool
}

type goPackage struct {
	importPath string
	name       string
}

// New returns a Generator.
func New(opts Options) *Generator {
	return &Generator{
		opts:      opts,
		pkgs:      map[*desc.File]goPackage{},
		msgNames:  map[*desc.Message]string{},
		enmNames:  map[*desc.Enum]string{},
		valNames:  map[*desc.EnumValue]string{},
		msgInfos:  map[*desc.Message]*messageInfo{},
		pkgIdents: map[string]map[string]bool{},
	}
}

func (g *Generator) goPackage(f *desc.File) (goPackage, error) {
	if p, ok := g.pkgs[f]; ok {
		return p, nil
	}
	spec, ok := g.opts.ImportMap[f.Path]
	if !ok {
		spec = f.GoPackage
	}
	if spec == "" {
		return goPackage{}, fmt.Errorf("%s: unable to determine Go import path; add 'option go_package = \"example.com/foo;foo\";' or pass -M %s=example.com/foo", f.Path, f.Path)
	}
	p := goPackage{importPath: spec}
	if i := strings.LastIndexByte(spec, ';'); i >= 0 {
		p.importPath, p.name = spec[:i], cleanPackageName(spec[i+1:])
	}
	if p.name == "" {
		p.name = defaultPackageName(p.importPath)
	}
	if strings.HasPrefix(p.importPath, "google.golang.org/protobuf/") || strings.HasPrefix(p.importPath, "github.com/golang/protobuf/") {
		return goPackage{}, fmt.Errorf("%s: Go import path %q belongs to the protobuf-go runtime, which cotorp does not use; generate this file with cotorp and map it with -M %s=<your/import/path>", f.Path, p.importPath, f.Path)
	}
	g.pkgs[f] = p
	return p, nil
}

// Generate produces Go files for the given files. Imported files must have
// been linked but are not themselves generated.
func (g *Generator) Generate(files []*desc.File) ([]OutputFile, error) {
	// Assign names for everything reachable, including dependencies.
	seen := map[*desc.File]bool{}
	var visit func(f *desc.File) error
	visit = func(f *desc.File) error {
		if seen[f] {
			return nil
		}
		seen[f] = true
		for _, d := range f.Imports {
			if err := visit(d); err != nil {
				return err
			}
		}
		g.assignNames(f)
		return nil
	}
	for _, f := range files {
		if err := visit(f); err != nil {
			return nil, err
		}
	}
	if err := g.checkJSONHex(seen); err != nil {
		return nil, err
	}

	var out []OutputFile
	dirs := map[string]string{} // output dir -> package name
	for _, f := range files {
		pkg, err := g.goPackage(f)
		if err != nil {
			return nil, err
		}
		name, err := g.outputName(f, pkg)
		if err != nil {
			return nil, err
		}
		dir := path.Dir(name)
		if prev, ok := dirs[dir]; ok && prev != pkg.name {
			return nil, fmt.Errorf("%s: Go package name %q conflicts with %q used by other files in %s", f.Path, pkg.name, prev, dir)
		} else if !ok {
			dirs[dir] = pkg.name
		}
		content, err := g.generateFile(f, pkg)
		if err != nil {
			return nil, err
		}
		out = append(out, OutputFile{Name: name, Content: content})
	}
	return out, nil
}

// checkJSONHex validates the JSONHex field names against the loaded files.
func (g *Generator) checkJSONHex(files map[*desc.File]bool) error {
	g.jsonHex = map[string]bool{}
	if len(g.opts.JSONHex) == 0 {
		return nil
	}
	fields := map[string]*desc.Field{}
	for f := range files {
		for _, m := range f.AllMessages {
			for _, fd := range m.Fields {
				fields[fd.FullName] = fd
			}
		}
	}
	for _, name := range g.opts.JSONHex {
		fd, ok := fields[name]
		switch {
		case !ok:
			return fmt.Errorf("-json_hex %s: no such field", name)
		case fd.IsMap || fd.Kind != desc.KindBytes:
			return fmt.Errorf("-json_hex %s: field must be a bytes field (singular or repeated), not %s", name, describeField(fd))
		}
		g.jsonHex[name] = true
	}
	return nil
}

func describeField(fd *desc.Field) string {
	switch {
	case fd.IsMap:
		return "a map"
	case fd.Kind == desc.KindMessage:
		return "message " + fd.MessageType.FullName
	case fd.Kind == desc.KindEnum:
		return "enum " + fd.EnumType.FullName
	}
	return fd.Kind.String()
}

func (g *Generator) outputName(f *desc.File, pkg goPackage) (string, error) {
	base := strings.TrimSuffix(f.Path, ".proto") + ".pb.go"
	if g.opts.Paths == "source_relative" {
		return base, nil
	}
	name := path.Join(pkg.importPath, path.Base(base))
	if m := g.opts.Module; m != "" {
		if !strings.HasPrefix(name, m+"/") {
			return "", fmt.Errorf("%s: Go import path %q does not have module prefix %q", f.Path, pkg.importPath, m)
		}
		name = strings.TrimPrefix(name, m+"/")
	}
	return name, nil
}

// assignNames computes Go identifiers for all types in f.
func (g *Generator) assignNames(f *desc.File) {
	pkg, _ := g.goPackage(f)
	idents := g.pkgIdents[pkg.importPath]
	if idents == nil {
		idents = map[string]bool{}
		g.pkgIdents[pkg.importPath] = idents
	}
	for _, m := range f.AllMessages {
		name := camelCase(m.Name)
		if m.Parent != nil {
			name = g.msgNames[m.Parent] + "_" + name
		}
		g.msgNames[m] = name
		idents[name] = true
	}
	for _, e := range f.AllEnums {
		name := camelCase(e.Name)
		prefix := name
		if e.Parent != nil {
			name = g.msgNames[e.Parent] + "_" + name
			prefix = g.msgNames[e.Parent]
		}
		g.enmNames[e] = name
		idents[name] = true
		for _, v := range e.Values {
			vn := prefix + "_" + v.Name
			g.valNames[v] = vn
			idents[vn] = true
		}
	}
	for _, m := range f.AllMessages {
		if !m.IsMapEntry {
			g.msgInfos[m] = g.newMessageInfo(m, idents)
		}
	}
}

// Method names generated on every message; fields are renamed to avoid them.
var reservedMethodNames = []string{
	"Reset",
	"MarshalBinary",
	"AppendBinary",
	"UnmarshalBinary",
	"ProtoSize",
	"ProtoMarshalToSizedBuffer",
	"ProtoMerge",
	"ProtoMergeDepth",
	"ProtoCheckInitialized",
	"ProtoUnknownFields",
	"MarshalJSON",
	"UnmarshalJSON",
	"ProtoAppendJSON",
	"ProtoMergeJSON",
}

type messageInfo struct {
	m      *desc.Message
	goName string
	fields []*fieldInfo // declaration order
	byNum  []*fieldInfo // field number order
	oneofs []*oneofInfo
}

type fieldInfo struct {
	f       *desc.Field
	goName  string
	getter  string
	oneof   *oneofInfo
	wrapper string // oneof wrapper type
	defName string // Default_ constant name, if any
}

type oneofInfo struct {
	o      *desc.Oneof
	goName string
	getter string
	iface  string
	fields []*fieldInfo
}

func (g *Generator) newMessageInfo(m *desc.Message, idents map[string]bool) *messageInfo {
	mi := &messageInfo{m: m, goName: g.msgNames[m]}
	used := map[string]bool{}
	for _, n := range reservedMethodNames {
		used[n] = true
	}
	unique := func(name string) string {
		for used[name] || used["Get"+name] {
			name += "_"
		}
		used[name] = true
		used["Get"+name] = true
		return name
	}
	oneofs := map[*desc.Oneof]*oneofInfo{}
	for _, f := range m.Fields {
		fi := &fieldInfo{f: f, goName: unique(camelCase(f.Name))}
		fi.getter = "Get" + fi.goName
		mi.fields = append(mi.fields, fi)
	}
	for _, o := range m.Oneofs {
		oi := &oneofInfo{o: o, goName: unique(camelCase(o.Name))}
		oi.getter = "Get" + oi.goName
		oi.iface = "is" + mi.goName + "_" + oi.goName
		oneofs[o] = oi
		mi.oneofs = append(mi.oneofs, oi)
	}
	for _, fi := range mi.fields {
		if fi.f.Oneof != nil {
			oi := oneofs[fi.f.Oneof]
			fi.oneof = oi
			oi.fields = append(oi.fields, fi)
			w := mi.goName + "_" + fi.goName
			for idents[w] {
				w += "_"
			}
			idents[w] = true
			fi.wrapper = w
		}
		if fi.f.Default != nil {
			fi.defName = "Default_" + mi.goName + "_" + fi.goName
		}
	}
	mi.byNum = append([]*fieldInfo(nil), mi.fields...)
	sort.SliceStable(mi.byNum, func(i, j int) bool { return mi.byNum[i].f.Number < mi.byNum[j].f.Number })
	return mi
}

// fileGen generates a single file.
type fileGen struct {
	g          *Generator
	f          *desc.File
	pkg        goPackage
	buf        bytes.Buffer
	imports    map[string]string // generated package import path -> alias
	aliases    map[string]bool
	stdImports map[string]bool // standard library import paths

	// usesU records use of the marshal scratch variable u in the function
	// body currently being generated.
	usesU bool
}

func (g *Generator) generateFile(f *desc.File, pkg goPackage) (_ []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			ge, ok := r.(genError)
			if !ok {
				panic(r)
			}
			err = fmt.Errorf("%s: %w", f.Path, ge.err)
		}
	}()
	fg := &fileGen{g: g, f: f, pkg: pkg, imports: map[string]string{}, aliases: map[string]bool{}, stdImports: map[string]bool{}}
	for _, e := range f.AllEnums {
		fg.genEnum(e)
	}
	for _, m := range f.AllMessages {
		if m.IsMapEntry {
			continue
		}
		fg.genMessage(g.msgInfos[m])
	}

	var hdr bytes.Buffer
	fmt.Fprintf(&hdr, "// Code generated by cotorp. DO NOT EDIT.\n// source: %s\n\n", f.Path)
	fmt.Fprintf(&hdr, "package %s\n\n", pkg.name)
	if len(fg.imports)+len(fg.stdImports) > 0 {
		hdr.WriteString("import (\n")
		std := make([]string, 0, len(fg.stdImports))
		for p := range fg.stdImports {
			std = append(std, p)
		}
		sort.Strings(std)
		for _, p := range std {
			fmt.Fprintf(&hdr, "\t%q\n", p)
		}
		paths := make([]string, 0, len(fg.imports))
		for p := range fg.imports {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		if len(std) > 0 && len(paths) > 0 {
			hdr.WriteString("\n")
		}
		for _, p := range paths {
			fmt.Fprintf(&hdr, "\t%s %q\n", fg.imports[p], p)
		}
		hdr.WriteString(")\n\n")
	}
	src := append(hdr.Bytes(), fg.buf.Bytes()...)
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: internal error: generated invalid Go code: %v\n%s", f.Path, err, numberLines(src))
	}
	return formatted, nil
}

func numberLines(src []byte) string {
	var b strings.Builder
	for i, line := range strings.Split(string(src), "\n") {
		fmt.Fprintf(&b, "%5d  %s\n", i+1, line)
	}
	return b.String()
}

func (fg *fileGen) P(args ...any) {
	for _, a := range args {
		fmt.Fprint(&fg.buf, a)
	}
	fg.buf.WriteByte('\n')
}

// capture runs fn and returns the code it generated instead of emitting it.
func (fg *fileGen) capture(fn func()) string {
	saved := fg.buf
	fg.buf = bytes.Buffer{}
	fn()
	out := fg.buf.String()
	fg.buf = saved
	return out
}

// Local variable names used in generated method bodies; import aliases
// must not shadow them.
var localNames = map[string]bool{
	"m": true, "b": true, "i": true, "n": true, "v": true, "x": true, "k": true,
	"j": true, "l": true, "o": true, "ok": true, "err": true, "start": true,
	"keys": true, "num": true, "typ": true, "depth": true, "mk": true, "mv": true,
	"size": true, "u": true, "t": true, "ln": true, "k2": true, "stk": true,
	"open": true, "e": true, "lv": true, "s": true,
	// JSON methods.
	"c": true, "ci": true, "fl": true, "d": true, "tok": true, "key": true,
	"raw": true, "job": true, "jobs": true, "jb": true, "seen": true,
	"oneofs": true, "f": true, "null": true, "ad": true, "kt": true,
	"ks": true, "class": true, "bits": true, "iv": true, "uv": true,
	"fv": true, "bv": true, "sv": true, "by": true, "r": true, "ev": true,
	"k64": true, "enc": true, "special": true, "ns": true, "secs": true,
	"nanos": true, "neg": true, "in": true, "whole": true, "frac": true,
	"dot": true, "p": true, "sb": true,
}

// qualify returns name as referenced from this file, adding an import if it
// lives in another Go package.
func (fg *fileGen) qualify(f *desc.File, name string) string {
	pkg, err := fg.g.goPackage(f)
	if err != nil {
		// Recovered in generateFile.
		panic(genError{err})
	}
	if pkg.importPath == fg.pkg.importPath {
		return name
	}
	alias, ok := fg.imports[pkg.importPath]
	if !ok {
		base := pkg.name
		alias = base
		for i := 1; fg.aliases[alias] || localNames[alias] || isStdName(alias) || alias == fg.pkg.name; i++ {
			alias = fmt.Sprintf("%s%d", base, i)
		}
		fg.aliases[alias] = true
		fg.imports[pkg.importPath] = alias
	}
	return alias + "." + name
}

type genError struct{ err error }

func isStdName(name string) bool {
	for _, n := range stdPackages {
		if n == name {
			return true
		}
	}
	return false
}

func (fg *fileGen) msgType(m *desc.Message) string {
	return fg.qualify(m.File, fg.g.msgNames[m])
}

func (fg *fileGen) enumType(e *desc.Enum) string {
	return fg.qualify(e.File, fg.g.enmNames[e])
}

func (fg *fileGen) enumValue(v *desc.EnumValue) string {
	return fg.qualify(v.Enum.File, fg.g.valNames[v])
}

func (fg *fileGen) comments(text string, deprecated bool) {
	if text != "" {
		for _, line := range strings.Split(text, "\n") {
			fg.P("//", line)
		}
	}
	if deprecated {
		if text != "" {
			fg.P("//")
		}
		fg.P("// Deprecated: Marked as deprecated in ", fg.f.Path, ".")
	}
}
