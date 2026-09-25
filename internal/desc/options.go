// Copyright (C) 2026 by Canonical Ltd
// Licensed under BSD-3-Clause, see LICENSE file for details.

package desc

import (
	"github.com/canonical/cotorp/internal/parser"
)

// Known option names per options message in descriptor.proto. Custom
// (parenthesised) options are accepted without validation.
var (
	fileOptions = set("java_package", "java_outer_classname", "java_multiple_files",
		"java_generate_equals_and_hash", "java_string_check_utf8", "optimize_for",
		"go_package", "cc_generic_services", "java_generic_services",
		"py_generic_services", "deprecated", "cc_enable_arenas", "objc_class_prefix",
		"csharp_namespace", "swift_prefix", "php_class_prefix", "php_namespace",
		"php_metadata_namespace", "ruby_package", "features", "uninterpreted_option")
	messageOptions = set("message_set_wire_format", "no_standard_descriptor_accessor",
		"deprecated", "map_entry", "deprecated_legacy_json_field_conflicts", "features",
		"uninterpreted_option")
	fieldOptions = set("ctype", "packed", "jstype", "lazy", "unverified_lazy",
		"deprecated", "weak", "debug_redact", "retention", "targets", "edition_defaults",
		"features", "feature_support", "uninterpreted_option",
		// Pseudo-options stored outside FieldOptions.
		"default", "json_name")
	oneofOptions     = set("features", "uninterpreted_option")
	enumOptions      = set("allow_alias", "deprecated", "deprecated_legacy_json_field_conflicts", "features", "uninterpreted_option")
	enumValueOptions = set("deprecated", "features", "debug_redact", "feature_support", "uninterpreted_option")
)

// repeatedOptions are repeated fields of the options messages, which may be
// assigned more than once.
var repeatedOptions = set("targets", "edition_defaults", "uninterpreted_option")

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func (b *builder) checkOptionNames(opts []*parser.Option, known map[string]bool, what string) {
	seen := map[string]bool{}
	for _, o := range opts {
		first := o.Name[0]
		if first.Ext {
			continue
		}
		if !known[first.Name] {
			b.errorf(o.Pos, "option %q unknown for %s", first.Name, what)
			continue
		}
		if len(o.Name) == 1 && first.Name != "features" && !repeatedOptions[first.Name] {
			if seen[first.Name] {
				b.errorf(o.Pos, "option %q was already set", first.Name)
			}
			seen[first.Name] = true
		}
	}
}

var featureValues = map[string]map[string]int{
	"field_presence":          {"EXPLICIT": 1, "IMPLICIT": 2, "LEGACY_REQUIRED": 3},
	"enum_type":               {"OPEN": 1, "CLOSED": 2},
	"repeated_field_encoding": {"PACKED": 1, "EXPANDED": 2},
	"utf8_validation":         {"VERIFY": 2, "NONE": 3},
	"message_encoding":        {"LENGTH_PREFIXED": 1, "DELIMITED": 2},
	"json_format":             {"ALLOW": 1, "LEGACY_BEST_EFFORT": 2},
	"enforce_naming_style":    {"STYLE2024": 1, "STYLE_LEGACY": 2},
	"default_symbol_visibility": {"EXPORT_ALL": 1, "EXPORT_TOP_LEVEL": 2,
		"LOCAL_ALL": 3, "STRICT": 4},
}

type featureInfo struct {
	values  map[string]int
	targets map[string]bool
	since   Edition
}

var allTargets = set("file", "extension_range", "message", "field", "oneof", "enum", "enum_entry", "service", "method")

// featureInfos mirrors the FeatureSet definition in descriptor.proto.
var featureInfos = map[string]featureInfo{
	"field_presence":            {featureValues["field_presence"], set("field", "file"), Edition2023},
	"enum_type":                 {featureValues["enum_type"], set("enum", "file"), Edition2023},
	"repeated_field_encoding":   {featureValues["repeated_field_encoding"], set("field", "file"), Edition2023},
	"utf8_validation":           {featureValues["utf8_validation"], set("field", "file"), Edition2023},
	"message_encoding":          {featureValues["message_encoding"], set("field", "file"), Edition2023},
	"json_format":               {featureValues["json_format"], set("message", "enum", "file"), Edition2023},
	"enforce_naming_style":      {featureValues["enforce_naming_style"], allTargets, Edition2024},
	"default_symbol_visibility": {featureValues["default_symbol_visibility"], set("file"), Edition2024},
}

// applyFeatures overlays any `features.*` options onto base. target names
// the kind of entity the options are attached to (e.g. "field").
func (b *builder) applyFeatures(base Features, opts []*parser.Option, target string) Features {
	whole := false
	set := map[string]bool{}
	for _, o := range opts {
		if o.Name[0] != (parser.OptionNamePart{Name: "features"}) {
			continue
		}
		key := "features"
		if len(o.Name) > 1 {
			key = optionName(o)
		}
		if whole || (key == "features" && len(set) > 0) || set[key] {
			b.errorf(o.Pos, "option %s was already set", key)
		}
		if key == "features" {
			whole = true
		}
		set[key] = true
	}
	for _, o := range opts {
		if o.Name[0] != (parser.OptionNamePart{Name: "features"}) {
			continue
		}
		if b.f.Edition < Edition2023 {
			b.errorf(o.Pos, "features are only valid under editions")
			continue
		}
		switch {
		case len(o.Name) == 1:
			if o.Value.Kind != parser.ValueMessage {
				b.errorf(o.Pos, "option features must be a message value")
				continue
			}
			for _, mf := range o.Value.Fields {
				if mf.Ext {
					continue // language-specific features
				}
				b.setFeature(&base, mf.Name, mf.Value, mf.Pos, target)
			}
		case o.Name[1].Ext:
			// Language-specific features such as features.(pb.go).x.
		case len(o.Name) == 2:
			b.setFeature(&base, o.Name[1].Name, o.Value, o.Pos, target)
		default:
			b.errorf(o.Pos, "invalid feature option %s", optionName(o))
		}
	}
	return base
}

func (b *builder) setFeature(f *Features, name string, v *parser.Value, pos parser.Pos, target string) {
	info, ok := featureInfos[name]
	if !ok {
		b.errorf(pos, "unknown feature %q", name)
		return
	}
	if b.f.Edition < info.since {
		b.errorf(pos, "feature %s was not introduced until edition %s", name, info.since)
		return
	}
	if !info.targets[target] {
		b.errorf(pos, "feature %s cannot be set on an entity of type %s", name, target)
		return
	}
	vals := info.values
	if v.Kind != parser.ValueIdent || v.Neg {
		b.errorf(pos, "feature %s must be an enum value", name)
		return
	}
	n, ok := vals[v.Ident]
	if !ok {
		b.errorf(pos, "invalid value %q for feature %s", v.Ident, name)
		return
	}
	switch name {
	case "field_presence":
		f.FieldPresence = FieldPresence(n)
	case "enum_type":
		f.EnumType = EnumType(n)
	case "repeated_field_encoding":
		f.RepeatedFieldEncoding = RepeatedFieldEncoding(n)
	case "utf8_validation":
		f.UTF8Validation = UTF8Validation(n)
	case "message_encoding":
		f.MessageEncoding = MessageEncoding(n)
	}
}
