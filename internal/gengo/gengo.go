// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package gengo generates Go code for protobuf messages. The generated code
// implements binary marshaling and unmarshaling and imports nothing outside
// the Go standard library (other than other generated packages). Each file is
// self-contained: wire format logic lives in the messages' methods and in
// unexported helper functions declared in the same file (see helpers.go).
package gengo

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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

	// Helpers is the Go import path (optionally "path;name") of a package
	// that holds the helper functions and constants shared by all generated
	// files, which import it instead of declaring their own. The package is
	// written by every Generate call, as helpers.pb.go under the import
	// path (with Module stripped). It is empty by default, and each file is
	// then self-contained.
	Helpers string
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
	// constPrefixes records the constant prefixes used per Go import path.
	constPrefixes map[string]map[string]bool
	// helpers is the shared helper package (see Options.Helpers), and
	// sharedErrs the error messages it declares, by constant name.
	helpers    *goPackage
	sharedErrs map[string]string
}

type goPackage struct {
	importPath string
	name       string
}

// New returns a Generator.
func New(opts Options) *Generator {
	return &Generator{
		opts:          opts,
		pkgs:          map[*desc.File]goPackage{},
		msgNames:      map[*desc.Message]string{},
		enmNames:      map[*desc.Enum]string{},
		valNames:      map[*desc.EnumValue]string{},
		msgInfos:      map[*desc.Message]*messageInfo{},
		pkgIdents:     map[string]map[string]bool{},
		constPrefixes: map[string]map[string]bool{},
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
	if g.opts.Helpers != "" {
		hf, err := g.generateHelpers()
		if err != nil {
			return nil, err
		}
		out = append(out, hf)
		dirs[path.Dir(hf.Name)] = g.helpers.name
	}
	for _, f := range files {
		pkg, err := g.goPackage(f)
		if err != nil {
			return nil, err
		}
		if g.helpers != nil && pkg.importPath == g.helpers.importPath {
			return nil, fmt.Errorf("%s: Go import path %q is the -helpers package", f.Path, pkg.importPath)
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
	name, err := g.importOutputName(pkg.importPath, path.Base(base))
	if err != nil {
		return "", fmt.Errorf("%s: %v", f.Path, err)
	}
	return name, nil
}

// importOutputName returns the output path of file base in the package at
// importPath, with the module prefix stripped.
func (g *Generator) importOutputName(importPath, base string) (string, error) {
	name := path.Join(importPath, base)
	if m := g.opts.Module; m != "" {
		if !strings.HasPrefix(name, m+"/") {
			return "", fmt.Errorf("Go import path %q does not have module prefix %q", importPath, m)
		}
		name = strings.TrimPrefix(name, m+"/")
	}
	return name, nil
}

// generateHelpers writes the shared helper package (see Options.Helpers):
// every helper and constant, exported, with the error messages the helpers
// use. Generated files refer to them through fn, c and errString.
func (g *Generator) generateHelpers() (OutputFile, error) {
	spec := g.opts.Helpers
	pkg := goPackage{importPath: spec}
	if i := strings.LastIndexByte(spec, ';'); i >= 0 {
		pkg.importPath, pkg.name = spec[:i], cleanPackageName(spec[i+1:])
	}
	if pkg.name == "" {
		pkg.name = defaultPackageName(pkg.importPath)
	}
	name, err := g.importOutputName(pkg.importPath, "helpers.pb.go")
	if err != nil {
		return OutputFile{}, fmt.Errorf("-helpers: %v", err)
	}
	g.helpers = &pkg
	if g.pkgIdents[pkg.importPath] == nil {
		g.pkgIdents[pkg.importPath] = map[string]bool{}
	}
	fg := &fileGen{g: g, pkg: pkg, shared: true, imports: map[string]string{}, aliases: map[string]bool{}, stdImports: map[string]bool{}}
	fg.consts = map[string]string{}
	fg.errIdx = map[string]int{}
	fg.funcs = map[string]string{}
	for _, cg := range constGroups {
		for _, c := range cg.consts {
			fg.c(c.suffix)
		}
	}
	for _, h := range helperDefs {
		fg.fn(h.suffix)
	}
	fg.genHelpers()
	body, errs := fg.resolveErrs(fg.buf.Bytes())
	g.sharedErrs = map[string]string{}
	for _, e := range fg.errs {
		g.sharedErrs[e.hint] = e.text
	}
	fg.dropUnusedImports(body)
	var hdr bytes.Buffer
	hdr.WriteString("// Code generated by cotorp. DO NOT EDIT.\n// helpers: shared by the packages generated with -helpers\n\n")
	fmt.Fprintf(&hdr, "// Package %s holds the helper functions and constants of the code that\n// cotorp generates with -helpers. It has no stable API.\npackage %s\n\n", pkg.name, pkg.name)
	fg.writeImports(&hdr)
	fg.writeConsts(&hdr, errs)
	src := append(hdr.Bytes(), body...)
	formatted, err := format.Source(src)
	if err != nil {
		return OutputFile{}, fmt.Errorf("-helpers: internal error: generated invalid Go code: %v\n%s", err, numberLines(src))
	}
	return OutputFile{Name: name, Content: formatted}, nil
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
	"MarshalJSONTo",
	"UnmarshalJSONFrom",
	"ProtoMergeJSONFrom",
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
		idents[oi.iface] = true
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

	// usesErr records use of the variable err in the ProtoAppendJSON body
	// currently being generated, and usesX that of x in ProtoMergeDepth.
	usesErr, usesX bool

	// enumJSON names the JSON parse function declared for each enum used
	// by the file (see enumJSONFunc).
	enumJSON map[*desc.Enum]string

	// shared marks the helper package itself (see generateHelpers), whose
	// helpers and constants are exported and unprefixed.
	shared bool

	// File-level constants (see consts.go).
	prefix string            // name prefix for this file's constants
	consts map[string]string // constGroups suffix -> declared name
	errs   []*errConst
	errIdx map[string]int // error text -> index in errs

	// decls holds package-level declarations to write after the message
	// being generated, and zeros the zero values of the file's messages,
	// declared together before the helpers.
	decls, zeros []string

	// Helper functions (see helpers.go).
	funcs map[string]string // helperDefs suffix -> declared name
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
	fg.prefix = g.newConstPrefix(f, pkg)
	fg.consts = map[string]string{}
	fg.errIdx = map[string]int{}
	fg.funcs = map[string]string{}
	fg.enumJSON = map[*desc.Enum]string{}
	for _, e := range f.AllEnums {
		fg.genEnum(e)
	}
	for _, m := range f.AllMessages {
		if m.IsMapEntry {
			continue
		}
		fg.genMessage(g.msgInfos[m])
	}
	if len(fg.zeros) > 0 {
		fg.P("var (")
		for _, z := range fg.zeros {
			fg.P(z)
		}
		fg.P(")")
		fg.P()
	}
	fg.genHelpers()
	body, errs := fg.resolveErrs(fg.buf.Bytes())
	fg.dropUnusedImports(body)

	var hdr bytes.Buffer
	fmt.Fprintf(&hdr, "// Code generated by cotorp. DO NOT EDIT.\n// source: %s\n\n", f.Path)
	fmt.Fprintf(&hdr, "package %s\n\n", pkg.name)
	fg.writeImports(&hdr)
	fg.writeConsts(&hdr, errs)
	src := append(hdr.Bytes(), body...)
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: internal error: generated invalid Go code: %v\n%s", f.Path, err, numberLines(src))
	}
	return formatted, nil
}

// writeImports writes the import declaration of the file's recorded
// imports.
func (fg *fileGen) writeImports(hdr *bytes.Buffer) {
	if len(fg.imports)+len(fg.stdImports) == 0 {
		return
	}
	hdr.WriteString("import (\n")
	std := make([]string, 0, len(fg.stdImports))
	for p := range fg.stdImports {
		std = append(std, p)
	}
	sort.Strings(std)
	for _, p := range std {
		fmt.Fprintf(hdr, "\t%q\n", p)
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
		fmt.Fprintf(hdr, "\t%s %q\n", fg.imports[p], p)
	}
	hdr.WriteString(")\n\n")
}

// dropUnusedImports removes recorded imports that body does not reference.
// An import is recorded when an expression naming its package is built, and
// the generator may build one it does not emit, which would otherwise be an
// "imported and not used" compile error.
func (fg *fileGen) dropUnusedImports(body []byte) {
	f, err := parser.ParseFile(token.NewFileSet(), "", append([]byte("package p\n"), body...), parser.SkipObjectResolution)
	if err != nil {
		return // reported when the file is formatted
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	// Import aliases never collide with locals (see localNames), and the
	// locals that share a standard package's name, such as bits, are never
	// used with a selector, so a selector on the name is a use of the
	// package.
	for p := range fg.stdImports {
		if !used[stdPackages[p]] {
			delete(fg.stdImports, p)
		}
	}
	for p, alias := range fg.imports {
		if !used[alias] {
			delete(fg.imports, p)
		}
	}
}

// newConstPrefix picks the constant name prefix for f, unique among the
// files generated into the same package by this Generator.
func (g *Generator) newConstPrefix(f *desc.File, pkg goPackage) string {
	used := g.constPrefixes[pkg.importPath]
	if used == nil {
		used = map[string]bool{}
		g.constPrefixes[pkg.importPath] = used
	}
	base := constPrefix(f.Path)
	prefix := base
	for i := 2; used[prefix]; i++ {
		prefix = fmt.Sprintf("%s%d", base, i)
	}
	used[prefix] = true
	return prefix
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
	"depth": true, "mk": true, "mv": true,
	"size": true, "t": true, "e": true, "lv": true, "s": true,
	// JSON methods.
	"c": true, "ci": true, "fl": true, "d": true, "tok": true, "key": true,
	"seen": true, "oneofs": true, "f": true, "kt": true, "class": true,
	"bits": true, "iv": true, "uv": true,
	"fv": true, "bv": true, "sv": true, "by": true, "r": true, "ev": true,
	"k64": true, "enc": true, "special": true, "ns": true, "secs": true,
	"nanos": true, "neg": true, "in": true, "whole": true, "frac": true,
	"dot": true, "p": true, "sb": true, "lax": true, "more": true,
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
	return fg.importAlias(pkg) + "." + name
}

// importAlias returns the name by which this file refers to pkg, adding the
// import.
func (fg *fileGen) importAlias(pkg goPackage) string {
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
	return alias
}

// helpersAlias returns the name of the shared helper package in this file.
func (fg *fileGen) helpersAlias() string {
	return fg.importAlias(*fg.g.helpers)
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
