// Package flow holds the parts of the flow engine that need nothing but data:
// filling in {{placeholders}}, checking conditions and working out when a
// schedule runs next.
package flow

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Vars is everything a placeholder can name. Roots: "trigger" (the event,
// webhook body or manual input), "steps" (each earlier step's output by step
// id), "flow", "me", plus "now", "today", "time". A name that is not a root is
// looked up in the trigger, so {{title}} is {{trigger.title}}.
type Vars map[string]any

var placeholder = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// Render fills in every {{path | filter}} in s. Unknown paths become "".
func Render(s string, v Vars) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		expr := placeholder.FindStringSubmatch(m)[1]
		parts := strings.Split(expr, "|")
		val, _ := Lookup(v, strings.TrimSpace(parts[0]))
		for _, f := range parts[1:] {
			val = applyFilter(val, strings.TrimSpace(f))
		}
		return Text(val)
	})
}

// RenderAll fills in every value of a config map.
func RenderAll(cfg map[string]string, v Vars) map[string]string {
	out := make(map[string]string, len(cfg))
	for k, s := range cfg {
		out[k] = Render(s, v)
	}
	return out
}

// Lookup finds a dotted path ("trigger.items.0.name"). Bare names fall back to the trigger.
func Lookup(v Vars, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	keys := strings.Split(path, ".")
	var cur any = map[string]any(v)
	if _, root := v[keys[0]]; !root {
		cur = v["trigger"]
	}
	for _, k := range keys {
		switch c := cur.(type) {
		case map[string]any:
			x, ok := c[k]
			if !ok {
				return nil, false
			}
			cur = x
		case Vars:
			x, ok := c[k]
			if !ok {
				return nil, false
			}
			cur = x
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// Text is how a value reads inside a sentence.
func Text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	case []any:
		parts := make([]string, 0, len(x))
		simple := true
		for _, e := range x {
			switch e.(type) {
			case map[string]any, []any:
				simple = false
			}
			parts = append(parts, Text(e))
		}
		if simple {
			return strings.Join(parts, ", ")
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func applyFilter(v any, f string) any {
	name, arg, _ := strings.Cut(f, ":")
	name = strings.ToLower(strings.TrimSpace(name))
	arg = strings.Trim(strings.TrimSpace(arg), `"'`)
	switch name {
	case "upper":
		return strings.ToUpper(Text(v))
	case "lower":
		return strings.ToLower(Text(v))
	case "trim":
		return strings.TrimSpace(Text(v))
	case "default":
		if Text(v) == "" {
			return arg
		}
		return v
	case "json":
		b, _ := json.Marshal(v)
		return string(b)
	case "truncate":
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 {
			n = 100
		}
		r := []rune(Text(v))
		if len(r) > n {
			return string(r[:n]) + "…"
		}
		return string(r)
	case "first_line":
		s, _, _ := strings.Cut(strings.TrimSpace(Text(v)), "\n")
		return s
	case "round":
		n, ok := Number(v)
		if !ok {
			return v
		}
		d, _ := strconv.Atoi(arg)
		p := math.Pow(10, float64(d))
		return strconv.FormatFloat(math.Round(n*p)/p, 'f', d, 64)
	case "cents":
		// minor units to a decimal amount: 1250 → 12.50
		n, ok := Number(v)
		if !ok {
			return v
		}
		return strconv.FormatFloat(n/100, 'f', 2, 64)
	case "date":
		t, ok := parseTime(Text(v))
		if !ok {
			return v
		}
		if arg == "" {
			arg = "2006-01-02"
		}
		return t.Format(goLayout(arg))
	case "count":
		switch x := v.(type) {
		case []any:
			return len(x)
		case map[string]any:
			return len(x)
		case string:
			return len([]rune(x))
		}
		return 0
	}
	return v
}

// goLayout turns a few friendly date words into Go layouts.
func goLayout(f string) string {
	r := strings.NewReplacer("YYYY", "2006", "MM", "01", "DD", "02", "HH", "15", "mm", "04", "ss", "05")
	return r.Replace(f)
}

func parseTime(s string) (time.Time, bool) {
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Number reads a value as a number ("12,50" and "€ 12.50" too).
func Number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case bool:
		return 0, false
	case string:
		s := strings.TrimSpace(x)
		s = strings.Trim(s, "€$£ ")
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(s, "EUR"), "KM"))
		if strings.Count(s, ",") == 1 && !strings.Contains(s, ".") {
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
	return 0, false
}

// Fields lists the dotted paths of a JSON object, for the field picker
// ("title", "price", "items.0.name"), at most depth levels deep.
func Fields(v any, depth int) []string {
	var out []string
	var walk func(prefix string, x any, d int)
	walk = func(prefix string, x any, d int) {
		switch c := x.(type) {
		case map[string]any:
			if d == 0 {
				if prefix != "" {
					out = append(out, prefix)
				}
				return
			}
			keys := make([]string, 0, len(c))
			for k := range c {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, c[k], d-1)
			}
		case []any:
			if prefix != "" {
				out = append(out, prefix)
			}
			if len(c) > 0 && d > 0 {
				if _, ok := c[0].(map[string]any); ok {
					walk(prefix+".0", c[0], d-1)
				}
			}
		default:
			if prefix != "" {
				out = append(out, prefix)
			}
		}
	}
	walk("", v, depth)
	return out
}

// Preview shortens a value for a run log line.
func Preview(v any, n int) string {
	s := Text(v)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Sprintf is fmt.Sprintf, kept here so callers need one import for log lines.
func Sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
