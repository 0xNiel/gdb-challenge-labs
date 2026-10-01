package manifest

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
)

// validate checks v against a JSON Schema node. It supports the keywords
// challenges/schema/*.schema.json use, and nothing else: type, required, properties,
// additionalProperties (false), pattern, enum, minimum, maximum, minLength, maxLength,
// items, minItems, maxItems. An unknown keyword is an error, so the schema cannot quietly
// rely on something this does not check.
func validate(schema map[string]any, v any, path string) []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, path+": "+fmt.Sprintf(format, a...)) }
	for k := range schema {
		switch k {
		case "$schema", "$id", "title", "description", "type", "required", "properties", "additionalProperties",
			"pattern", "enum", "minimum", "maximum", "minLength", "maxLength", "items", "minItems", "maxItems":
		default:
			add("schema keyword %q is not supported by manifestlint", k)
		}
	}
	if t, ok := schema["type"].(string); ok && !isType(v, t) {
		add("want %s, got %s", t, typeName(v))
		return errs
	}
	if enum, ok := schema["enum"].([]any); ok && !slices.ContainsFunc(enum, func(e any) bool { return fmt.Sprint(e) == fmt.Sprint(v) }) {
		add("%v is not one of %v", v, enum)
	}
	switch x := v.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		for _, r := range asStrings(schema["required"]) {
			if _, ok := x[r]; !ok {
				add("missing %q", r)
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub, ok := props[k].(map[string]any)
			if !ok {
				if schema["additionalProperties"] == false {
					add("unknown key %q", k)
				}
				continue
			}
			errs = append(errs, validate(sub, x[k], path+"."+k)...)
		}
	case []any:
		if n, ok := num(schema["minItems"]); ok && float64(len(x)) < n {
			add("%d items, at least %s", len(x), fmtNum(n))
		}
		if n, ok := num(schema["maxItems"]); ok && float64(len(x)) > n {
			add("%d items, at most %s", len(x), fmtNum(n))
		}
		if item, ok := schema["items"].(map[string]any); ok {
			for i, e := range x {
				errs = append(errs, validate(item, e, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case string:
		if p, ok := schema["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(x) {
			add("%q does not match %s", x, p)
		}
		if n, ok := num(schema["minLength"]); ok && float64(len([]rune(x))) < n {
			add("shorter than %s", fmtNum(n))
		}
		if n, ok := num(schema["maxLength"]); ok && float64(len([]rune(x))) > n {
			add("longer than %s", fmtNum(n))
		}
	default:
		if f, ok := num(v); ok {
			if n, ok := num(schema["minimum"]); ok && f < n {
				add("%s is below %s", fmtNum(f), fmtNum(n))
			}
			if n, ok := num(schema["maximum"]); ok && f > n {
				add("%s is above %s", fmtNum(f), fmtNum(n))
			}
		}
	}
	return errs
}

func isType(v any, t string) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		f, ok := num(v)
		return ok && f == math.Trunc(f)
	case "number":
		_, ok := num(v)
		return ok
	}
	return false
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	return fmt.Sprintf("%T", v)
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func asStrings(v any) []string {
	var out []string
	for _, x := range asSlice(v) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func asSlice(v any) []any { s, _ := v.([]any); return s }
