// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

// Package parser implements a parser for the protocol buffers language
// (proto2, proto3 and editions).
package parser

import (
	"math"
	"strconv"
	"strings"
)

type parser struct {
	file *File
	toks []token
	i    int
}

// Parse parses the .proto source src, reporting positions relative to name.
func Parse(name, src string) (*File, error) {
	toks, err := tokenize(name, src)
	if err != nil {
		return nil, err
	}
	p := &parser{file: &File{Name: name}, toks: toks}
	if err := p.parseFile(); err != nil {
		return nil, err
	}
	return p.file, nil
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) peekN(n int) token {
	if p.i+n < len(p.toks) {
		return p.toks[p.i+n]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

func (p *parser) isSym(s string) bool {
	t := p.peek()
	return t.kind == tokSymbol && t.text == s
}

func (p *parser) isKeyword(s string) bool {
	t := p.peek()
	return t.kind == tokIdent && t.text == s
}

func (p *parser) acceptSym(s string) bool {
	if p.isSym(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) errUnexpected(want string) error {
	t := p.peek()
	return Errorf(t.pos, "expected %s, found %s", want, t.describe())
}

func (p *parser) expectSym(s string) (token, error) {
	if !p.isSym(s) {
		return token{}, p.errUnexpected(strconv.Quote(s))
	}
	return p.next(), nil
}

func (p *parser) expectKeyword(s string) error {
	if !p.isKeyword(s) {
		return p.errUnexpected(strconv.Quote(s))
	}
	p.next()
	return nil
}

func (p *parser) expectIdent() (token, error) {
	if p.peek().kind != tokIdent {
		return token{}, p.errUnexpected("identifier")
	}
	return p.next(), nil
}

// parseFullIdent parses ident ("." ident)*.
func (p *parser) parseFullIdent() (string, Pos, error) {
	t, err := p.expectIdent()
	if err != nil {
		return "", Pos{}, err
	}
	name := t.text
	for p.isSym(".") {
		p.next()
		t, err := p.expectIdent()
		if err != nil {
			return "", Pos{}, err
		}
		name += "." + t.text
	}
	return name, t.pos, nil
}

// parseTypeName parses ["."] fullIdent.
func (p *parser) parseTypeName() (string, Pos, error) {
	pos := p.peek().pos
	prefix := ""
	if p.acceptSym(".") {
		prefix = "."
	}
	name, _, err := p.parseFullIdent()
	if err != nil {
		return "", Pos{}, err
	}
	return prefix + name, pos, nil
}

func (p *parser) parseStringLit() (string, Pos, error) {
	t := p.peek()
	if t.kind != tokString {
		return "", Pos{}, p.errUnexpected("string")
	}
	var b strings.Builder
	for p.peek().kind == tokString {
		b.WriteString(p.next().text)
	}
	return b.String(), t.pos, nil
}

func (p *parser) parseFile() error {
	f := p.file
	f.Syntax = "proto2"
	first := true
	for {
		t := p.peek()
		if t.kind == tokEOF {
			return nil
		}
		if p.acceptSym(";") {
			continue
		}
		if t.kind != tokIdent {
			return p.errUnexpected("top-level statement")
		}
		switch t.text {
		case "syntax", "edition":
			if !first {
				return Errorf(t.pos, "%s statement must be the first statement in the file", t.text)
			}
			p.next()
			if _, err := p.expectSym("="); err != nil {
				return err
			}
			s, pos, err := p.parseStringLit()
			if err != nil {
				return err
			}
			if _, err := p.expectSym(";"); err != nil {
				return err
			}
			f.SyntaxPos = pos
			if t.text == "syntax" {
				if s != "proto2" && s != "proto3" {
					return Errorf(pos, "unrecognized syntax %q", s)
				}
				f.Syntax = s
			} else {
				f.Syntax = "editions"
				f.Edition = s
			}
		case "package":
			p.next()
			if f.Package != "" {
				return Errorf(t.pos, "multiple package statements")
			}
			name, _, err := p.parseFullIdent()
			if err != nil {
				return err
			}
			if _, err := p.expectSym(";"); err != nil {
				return err
			}
			f.Package = name
			f.PackagePos = t.pos
		case "import":
			p.next()
			imp := &Import{Pos: t.pos}
			switch {
			case p.isKeyword("public"):
				p.next()
				imp.Public = true
			case p.isKeyword("weak"):
				p.next()
				imp.Weak = true
			case p.isKeyword("option"):
				p.next()
				imp.Option = true
			}
			s, _, err := p.parseStringLit()
			if err != nil {
				return err
			}
			if _, err := p.expectSym(";"); err != nil {
				return err
			}
			imp.Path = s
			f.Imports = append(f.Imports, imp)
		case "option":
			opt, err := p.parseOptionStatement()
			if err != nil {
				return err
			}
			f.Options = append(f.Options, opt)
		case "message":
			m, err := p.parseMessage()
			if err != nil {
				return err
			}
			f.Messages = append(f.Messages, m)
		case "enum":
			e, err := p.parseEnum()
			if err != nil {
				return err
			}
			f.Enums = append(f.Enums, e)
		case "extend":
			e, err := p.parseExtend()
			if err != nil {
				return err
			}
			f.Extends = append(f.Extends, e)
		case "service":
			s, err := p.parseService()
			if err != nil {
				return err
			}
			f.Services = append(f.Services, s)
		case "export", "local":
			// Edition 2024 symbol visibility.
			p.next()
			switch {
			case p.isKeyword("message"):
				m, err := p.parseMessage()
				if err != nil {
					return err
				}
				f.Messages = append(f.Messages, m)
			case p.isKeyword("enum"):
				e, err := p.parseEnum()
				if err != nil {
					return err
				}
				f.Enums = append(f.Enums, e)
			default:
				return p.errUnexpected(`"message" or "enum"`)
			}
		default:
			return p.errUnexpected("top-level statement")
		}
		first = false
	}
}

// parseOptionStatement parses `option name = value ;`.
func (p *parser) parseOptionStatement() (*Option, error) {
	if err := p.expectKeyword("option"); err != nil {
		return nil, err
	}
	opt, err := p.parseOption()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectSym(";"); err != nil {
		return nil, err
	}
	return opt, nil
}

// parseOption parses `name = value`.
func (p *parser) parseOption() (*Option, error) {
	pos := p.peek().pos
	var name []OptionNamePart
	for {
		if p.acceptSym("(") {
			n, _, err := p.parseTypeName()
			if err != nil {
				return nil, err
			}
			if _, err := p.expectSym(")"); err != nil {
				return nil, err
			}
			name = append(name, OptionNamePart{Name: n, Ext: true})
		} else {
			t, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			name = append(name, OptionNamePart{Name: t.text})
		}
		if !p.acceptSym(".") {
			break
		}
	}
	if _, err := p.expectSym("="); err != nil {
		return nil, err
	}
	v, err := p.parseConstant()
	if err != nil {
		return nil, err
	}
	return &Option{Pos: pos, Name: name, Value: v}, nil
}

// parseOptionList parses `[ opt, opt ]` if present.
func (p *parser) parseOptionList() ([]*Option, error) {
	if !p.acceptSym("[") {
		return nil, nil
	}
	var opts []*Option
	for {
		o, err := p.parseOption()
		if err != nil {
			return nil, err
		}
		opts = append(opts, o)
		if p.acceptSym(",") {
			continue
		}
		if _, err := p.expectSym("]"); err != nil {
			return nil, err
		}
		return opts, nil
	}
}

func parseIntLit(text string) (uint64, bool) {
	var v uint64
	var err error
	switch {
	case strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X"):
		v, err = strconv.ParseUint(text[2:], 16, 64)
	case len(text) > 1 && text[0] == '0':
		v, err = strconv.ParseUint(text[1:], 8, 64)
	default:
		v, err = strconv.ParseUint(text, 10, 64)
	}
	return v, err == nil
}

// parseConstant parses a scalar constant or a text-format aggregate.
func (p *parser) parseConstant() (*Value, error) {
	t := p.peek()
	if t.kind == tokSymbol && t.text == "{" {
		p.next()
		fields, err := p.parseTextMessage("}")
		if err != nil {
			return nil, err
		}
		return &Value{Pos: t.pos, Kind: ValueMessage, Fields: fields}, nil
	}
	return p.parseScalar()
}

// parseScalar parses a scalar constant: [-+] number, [-+] ident, ident or
// string.
func (p *parser) parseScalar() (*Value, error) {
	t := p.peek()
	neg := false
	if t.kind == tokSymbol && (t.text == "-" || t.text == "+") {
		p.next()
		neg = t.text == "-"
	}
	n := p.peek()
	switch n.kind {
	case tokInt:
		p.next()
		v, ok := parseIntLit(n.text)
		if !ok {
			// Too large for uint64: treat as a float.
			f, err := strconv.ParseFloat(n.text, 64)
			if err != nil {
				return nil, Errorf(n.pos, "invalid integer %q", n.text)
			}
			if neg {
				f = -f
			}
			return &Value{Pos: t.pos, Kind: ValueFloat, Float: f, Neg: neg, Text: n.text}, nil
		}
		return &Value{Pos: t.pos, Kind: ValueInt, Int: v, Neg: neg, Text: n.text}, nil
	case tokFloat:
		p.next()
		f, err := strconv.ParseFloat(n.text, 64)
		if err != nil && !isRangeErr(err) {
			return nil, Errorf(n.pos, "invalid float %q", n.text)
		}
		if neg {
			f = -f
		}
		return &Value{Pos: t.pos, Kind: ValueFloat, Float: f, Neg: neg, Text: n.text}, nil
	case tokIdent:
		p.next()
		name := n.text
		// Allow qualified identifiers for enum values in aggregates.
		for p.isSym(".") && p.peekN(1).kind == tokIdent {
			p.next()
			name += "." + p.next().text
		}
		if neg {
			switch strings.ToLower(name) {
			case "inf", "infinity":
				return &Value{Pos: t.pos, Kind: ValueFloat, Float: math.Inf(-1), Neg: true, Ident: name}, nil
			case "nan":
				return &Value{Pos: t.pos, Kind: ValueFloat, Float: math.NaN(), Neg: true, Ident: name}, nil
			}
		}
		return &Value{Pos: t.pos, Kind: ValueIdent, Ident: name, Neg: neg}, nil
	case tokString:
		if neg {
			return nil, p.errUnexpected("number")
		}
		s, _, err := p.parseStringLit()
		if err != nil {
			return nil, err
		}
		return &Value{Pos: t.pos, Kind: ValueString, Str: s}, nil
	}
	return nil, p.errUnexpected("constant")
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

// parseTextMessage parses text-format fields until the closing delimiter.
func (p *parser) parseTextMessage(end string) ([]*MessageField, error) {
	var fields []*MessageField
	for {
		if p.acceptSym(end) {
			return fields, nil
		}
		t := p.peek()
		if t.kind == tokEOF {
			return nil, p.errUnexpected(strconv.Quote(end))
		}
		mf := &MessageField{Pos: t.pos}
		if p.acceptSym("[") {
			// Extension name or Any type URL: consume tokens until ']'.
			var b strings.Builder
			for !p.isSym("]") {
				tt := p.next()
				if tt.kind == tokEOF {
					return nil, p.errUnexpected(`"]"`)
				}
				b.WriteString(tt.text)
			}
			p.next()
			mf.Name = b.String()
			mf.Ext = true
		} else {
			id, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			mf.Name = id.text
		}
		hasColon := p.acceptSym(":")
		v, err := p.parseTextValue(hasColon)
		if err != nil {
			return nil, err
		}
		mf.Value = v
		fields = append(fields, mf)
		if !p.acceptSym(",") {
			p.acceptSym(";")
		}
	}
}

func (p *parser) parseTextValue(hasColon bool) (*Value, error) {
	t := p.peek()
	switch {
	case p.isSym("{"):
		p.next()
		fs, err := p.parseTextMessage("}")
		if err != nil {
			return nil, err
		}
		return &Value{Pos: t.pos, Kind: ValueMessage, Fields: fs}, nil
	case p.isSym("<"):
		p.next()
		fs, err := p.parseTextMessage(">")
		if err != nil {
			return nil, err
		}
		return &Value{Pos: t.pos, Kind: ValueMessage, Fields: fs}, nil
	case p.isSym("["):
		p.next()
		lv := &Value{Pos: t.pos, Kind: ValueList}
		if p.acceptSym("]") {
			return lv, nil
		}
		for {
			v, err := p.parseTextValue(true)
			if err != nil {
				return nil, err
			}
			lv.List = append(lv.List, v)
			if p.acceptSym(",") {
				continue
			}
			if _, err := p.expectSym("]"); err != nil {
				return nil, err
			}
			return lv, nil
		}
	}
	if !hasColon {
		return nil, p.errUnexpected(`":"`)
	}
	return p.parseScalar()
}

func (p *parser) parseMessage() (*Message, error) {
	t := p.next() // "message"
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	m := &Message{Pos: name.pos, Comments: t.comments, Name: name.text}
	if _, err := p.expectSym("{"); err != nil {
		return nil, err
	}
	if err := p.parseMessageBody(m); err != nil {
		return nil, err
	}
	return m, nil
}

// parseMessageBody parses message elements up to and including the closing
// brace.
func (p *parser) parseMessageBody(m *Message) error {
	for {
		t := p.peek()
		if t.kind == tokEOF {
			return p.errUnexpected(`"}"`)
		}
		if p.acceptSym("}") {
			return nil
		}
		if p.acceptSym(";") {
			continue
		}
		if t.kind != tokIdent {
			if t.kind == tokSymbol && t.text == "." {
				// Fully-qualified field type.
				f, err := p.parseField(nil)
				if err != nil {
					return err
				}
				m.Fields = append(m.Fields, f)
				continue
			}
			return p.errUnexpected("message element")
		}
		switch t.text {
		case "option":
			o, err := p.parseOptionStatement()
			if err != nil {
				return err
			}
			m.Options = append(m.Options, o)
			continue
		case "message":
			if p.peekN(1).kind == tokIdent {
				nm, err := p.parseMessage()
				if err != nil {
					return err
				}
				m.Messages = append(m.Messages, nm)
				continue
			}
		case "enum":
			if p.peekN(1).kind == tokIdent {
				e, err := p.parseEnum()
				if err != nil {
					return err
				}
				m.Enums = append(m.Enums, e)
				continue
			}
		case "export", "local":
			if n := p.peekN(1); n.kind == tokIdent && (n.text == "message" || n.text == "enum") && p.peekN(2).kind == tokIdent {
				p.next()
				if n.text == "message" {
					nm, err := p.parseMessage()
					if err != nil {
						return err
					}
					m.Messages = append(m.Messages, nm)
				} else {
					e, err := p.parseEnum()
					if err != nil {
						return err
					}
					m.Enums = append(m.Enums, e)
				}
				continue
			}
		case "extend":
			if p.peekN(1).kind == tokIdent || (p.peekN(1).kind == tokSymbol && p.peekN(1).text == ".") {
				e, err := p.parseExtend()
				if err != nil {
					return err
				}
				m.Extends = append(m.Extends, e)
				continue
			}
		case "extensions":
			if n := p.peekN(1); n.kind == tokInt {
				p.next()
				rs, err := p.parseRanges(536870911)
				if err != nil {
					return err
				}
				// Extension range options are accepted and ignored.
				if _, err := p.parseOptionList(); err != nil {
					return err
				}
				if _, err := p.expectSym(";"); err != nil {
					return err
				}
				m.ExtensionRanges = append(m.ExtensionRanges, rs...)
				continue
			}
		case "reserved":
			if n := p.peekN(1); n.kind == tokInt || n.kind == tokString || (n.kind == tokIdent && p.peekN(2).kind == tokSymbol && (p.peekN(2).text == ";" || p.peekN(2).text == ",")) {
				p.next()
				rs, names, err := p.parseReserved(536870911)
				if err != nil {
					return err
				}
				m.ReservedRanges = append(m.ReservedRanges, rs...)
				m.ReservedNames = append(m.ReservedNames, names...)
				continue
			}
		case "oneof":
			if p.peekN(1).kind == tokIdent && p.peekN(2).kind == tokSymbol && p.peekN(2).text == "{" {
				o, err := p.parseOneof(m)
				if err != nil {
					return err
				}
				m.Oneofs = append(m.Oneofs, o)
				continue
			}
		}
		f, err := p.parseField(nil)
		if err != nil {
			return err
		}
		m.Fields = append(m.Fields, f)
	}
}

// parseRanges parses `N [to (N|max)] {, ...}`.
func (p *parser) parseRanges(max int64) ([]*Range, error) {
	var rs []*Range
	for {
		start, pos, err := p.parseRangeInt()
		if err != nil {
			return nil, err
		}
		r := &Range{Pos: pos, Start: start, End: start}
		if p.isKeyword("to") {
			p.next()
			if p.isKeyword("max") {
				p.next()
				r.End = max
				r.Max = true
			} else {
				end, _, err := p.parseRangeInt()
				if err != nil {
					return nil, err
				}
				r.End = end
			}
		}
		rs = append(rs, r)
		if !p.acceptSym(",") {
			return rs, nil
		}
	}
}

func (p *parser) parseRangeInt() (int64, Pos, error) {
	t := p.peek()
	neg := false
	if t.kind == tokSymbol && t.text == "-" {
		p.next()
		neg = true
	}
	n := p.peek()
	if n.kind != tokInt {
		return 0, Pos{}, p.errUnexpected("integer")
	}
	p.next()
	v, ok := parseIntLit(n.text)
	if !ok || v > math.MaxInt64 {
		return 0, Pos{}, Errorf(n.pos, "integer out of range")
	}
	if neg {
		return -int64(v), t.pos, nil
	}
	return int64(v), t.pos, nil
}

// parseReserved parses the body of a reserved statement (after the keyword).
func (p *parser) parseReserved(max int64) ([]*Range, []string, error) {
	var names []string
	if p.peek().kind == tokString || p.peek().kind == tokIdent {
		for {
			var name string
			if p.peek().kind == tokString {
				s, _, err := p.parseStringLit()
				if err != nil {
					return nil, nil, err
				}
				name = s
			} else {
				t, err := p.expectIdent()
				if err != nil {
					return nil, nil, err
				}
				name = t.text
			}
			names = append(names, name)
			if !p.acceptSym(",") {
				break
			}
		}
		if _, err := p.expectSym(";"); err != nil {
			return nil, nil, err
		}
		return nil, names, nil
	}
	rs, err := p.parseRanges(max)
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.expectSym(";"); err != nil {
		return nil, nil, err
	}
	return rs, nil, nil
}

func (p *parser) parseOneof(m *Message) (*Oneof, error) {
	t := p.next() // "oneof"
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	o := &Oneof{Pos: name.pos, Comments: t.comments, Name: name.text}
	if _, err := p.expectSym("{"); err != nil {
		return nil, err
	}
	for {
		if p.acceptSym("}") {
			return o, nil
		}
		if p.acceptSym(";") {
			continue
		}
		if p.isKeyword("option") {
			opt, err := p.parseOptionStatement()
			if err != nil {
				return nil, err
			}
			o.Options = append(o.Options, opt)
			continue
		}
		if p.peek().kind == tokEOF {
			return nil, p.errUnexpected(`"}"`)
		}
		f, err := p.parseField(o)
		if err != nil {
			return nil, err
		}
		f.Oneof = o
		o.Fields = append(o.Fields, f)
		m.Fields = append(m.Fields, f)
	}
}

// parseField parses a normal, group or map field.
func (p *parser) parseField(oneof *Oneof) (*Field, error) {
	start := p.peek()
	f := &Field{Pos: start.pos, Comments: start.comments}
	if start.kind == tokIdent {
		switch start.text {
		case "optional", "required", "repeated":
			if oneof != nil {
				return nil, Errorf(start.pos, "fields in oneofs must not have labels")
			}
			p.next()
			switch start.text {
			case "optional":
				f.Label = LabelOptional
			case "required":
				f.Label = LabelRequired
			default:
				f.Label = LabelRepeated
			}
		}
	}

	// Group field: group Name = N [opts] { body }
	if p.isKeyword("group") && p.peekN(1).kind == tokIdent && p.peekN(2).kind == tokSymbol && p.peekN(2).text == "=" && p.file.Syntax == "proto2" {
		p.next()
		name, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectSym("="); err != nil {
			return nil, err
		}
		num, err := p.parseFieldNumber()
		if err != nil {
			return nil, err
		}
		opts, err := p.parseOptionList()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectSym("{"); err != nil {
			return nil, err
		}
		body := &Message{Pos: name.pos, Comments: f.Comments, Name: name.text}
		if err := p.parseMessageBody(body); err != nil {
			return nil, err
		}
		if name.text == "" || !(name.text[0] >= 'A' && name.text[0] <= 'Z') {
			return nil, Errorf(name.pos, "group names must start with a capital letter")
		}
		f.Name = strings.ToLower(name.text)
		f.Type = name.text
		f.TypePos = name.pos
		f.Number = num
		f.Options = opts
		f.Group = body
		return f, nil
	}

	// Map field: map<K, V> name = N [opts];
	if p.isKeyword("map") && p.peekN(1).kind == tokSymbol && p.peekN(1).text == "<" {
		p.next()
		p.next()
		kt, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectSym(","); err != nil {
			return nil, err
		}
		vt, vpos, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectSym(">"); err != nil {
			return nil, err
		}
		f.IsMap = true
		f.MapKey = kt.text
		f.MapValue = vt
		f.TypePos = vpos
	} else {
		typ, pos, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		f.Type = typ
		f.TypePos = pos
	}
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	f.Name = name.text
	f.Pos = name.pos
	if _, err := p.expectSym("="); err != nil {
		return nil, err
	}
	num, err := p.parseFieldNumber()
	if err != nil {
		return nil, err
	}
	f.Number = num
	opts, err := p.parseOptionList()
	if err != nil {
		return nil, err
	}
	f.Options = opts
	if _, err := p.expectSym(";"); err != nil {
		return nil, err
	}
	return f, nil
}

func (p *parser) parseFieldNumber() (int32, error) {
	t := p.peek()
	if t.kind != tokInt {
		return 0, p.errUnexpected("field number")
	}
	p.next()
	v, ok := parseIntLit(t.text)
	if !ok || v > math.MaxInt32 {
		return 0, Errorf(t.pos, "field number %s out of range", t.text)
	}
	return int32(v), nil
}

func (p *parser) parseEnum() (*Enum, error) {
	t := p.next() // "enum"
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	e := &Enum{Pos: name.pos, Comments: t.comments, Name: name.text}
	if _, err := p.expectSym("{"); err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if p.acceptSym("}") {
			return e, nil
		}
		if p.acceptSym(";") {
			continue
		}
		if t.kind != tokIdent {
			return nil, p.errUnexpected("enum value")
		}
		if t.text == "option" && !(p.peekN(1).kind == tokSymbol && p.peekN(1).text == "=") {
			o, err := p.parseOptionStatement()
			if err != nil {
				return nil, err
			}
			e.Options = append(e.Options, o)
			continue
		}
		if t.text == "reserved" && !(p.peekN(1).kind == tokSymbol && p.peekN(1).text == "=") {
			p.next()
			rs, names, err := p.parseReserved(math.MaxInt32)
			if err != nil {
				return nil, err
			}
			e.ReservedRanges = append(e.ReservedRanges, rs...)
			e.ReservedNames = append(e.ReservedNames, names...)
			continue
		}
		p.next()
		v := &EnumValue{Pos: t.pos, Comments: t.comments, Name: t.text}
		if _, err := p.expectSym("="); err != nil {
			return nil, err
		}
		n, _, err := p.parseRangeInt()
		if err != nil {
			return nil, err
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return nil, Errorf(t.pos, "enum value %s out of range", t.text)
		}
		v.Number = int32(n)
		opts, err := p.parseOptionList()
		if err != nil {
			return nil, err
		}
		v.Options = opts
		if _, err := p.expectSym(";"); err != nil {
			return nil, err
		}
		e.Values = append(e.Values, v)
	}
}

func (p *parser) parseExtend() (*Extend, error) {
	t := p.next() // "extend"
	name, _, err := p.parseTypeName()
	if err != nil {
		return nil, err
	}
	e := &Extend{Pos: t.pos, Extendee: name}
	if _, err := p.expectSym("{"); err != nil {
		return nil, err
	}
	for {
		if p.acceptSym("}") {
			return e, nil
		}
		if p.acceptSym(";") {
			continue
		}
		if p.peek().kind == tokEOF {
			return nil, p.errUnexpected(`"}"`)
		}
		f, err := p.parseField(nil)
		if err != nil {
			return nil, err
		}
		e.Fields = append(e.Fields, f)
	}
}

// parseService parses and discards a service definition.
func (p *parser) parseService() (*Service, error) {
	p.next() // "service"
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectSym("{"); err != nil {
		return nil, err
	}
	depth := 1
	for depth > 0 {
		t := p.next()
		switch {
		case t.kind == tokEOF:
			return nil, Errorf(t.pos, "unterminated service %s", name.text)
		case t.kind == tokSymbol && t.text == "{":
			depth++
		case t.kind == tokSymbol && t.text == "}":
			depth--
		}
	}
	return &Service{Pos: name.pos, Name: name.text}, nil
}
