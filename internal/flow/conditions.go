package flow

import (
	"regexp"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// Ops are the comparisons a condition can make, with the words the UI shows.
var Ops = []struct{ Key, Name string }{
	{"equals", "is"},
	{"not_equals", "is not"},
	{"contains", "contains"},
	{"not_contains", "does not contain"},
	{"starts_with", "starts with"},
	{"ends_with", "ends with"},
	{"gt", "is more than"},
	{"gte", "is at least"},
	{"lt", "is less than"},
	{"lte", "is at most"},
	{"exists", "has a value"},
	{"missing", "is empty"},
	{"matches", "matches the pattern"},
}

// ValidOp reports whether op is a known comparison.
func ValidOp(op string) bool {
	for _, o := range Ops {
		if o.Key == op {
			return true
		}
	}
	return false
}

// OpName is the plain words for an op.
func OpName(op string) string {
	for _, o := range Ops {
		if o.Key == op {
			return o.Name
		}
	}
	return op
}

// Check evaluates one condition. The value may hold {{placeholders}}.
func Check(c store.Condition, v Vars) bool {
	got, ok := Lookup(v, c.Field)
	text := strings.TrimSpace(Text(got))
	want := strings.TrimSpace(Render(c.Value, v))
	switch c.Op {
	case "exists":
		return ok && text != ""
	case "missing":
		return !ok || text == ""
	case "equals":
		if a, okA := Number(got); okA {
			if b, okB := Number(want); okB {
				return a == b
			}
		}
		return strings.EqualFold(text, want)
	case "not_equals":
		return !Check(store.Condition{Field: c.Field, Op: "equals", Value: c.Value}, v)
	case "contains":
		return strings.Contains(strings.ToLower(text), strings.ToLower(want))
	case "not_contains":
		return !strings.Contains(strings.ToLower(text), strings.ToLower(want))
	case "starts_with":
		return strings.HasPrefix(strings.ToLower(text), strings.ToLower(want))
	case "ends_with":
		return strings.HasSuffix(strings.ToLower(text), strings.ToLower(want))
	case "gt", "gte", "lt", "lte":
		a, okA := Number(got)
		b, okB := Number(want)
		if !okA || !okB {
			return false
		}
		switch c.Op {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		}
		return a <= b
	case "matches":
		re, err := regexp.Compile("(?i)" + want)
		return err == nil && re.MatchString(text)
	}
	return false
}

// CheckAll evaluates a list: match "any" needs one, anything else needs all.
// No conditions = true.
func CheckAll(cs []store.Condition, match string, v Vars) bool {
	if len(cs) == 0 {
		return true
	}
	for _, c := range cs {
		ok := Check(c, v)
		if match == "any" && ok {
			return true
		}
		if match != "any" && !ok {
			return false
		}
	}
	return match != "any"
}

// Describe says a condition in words: `price is less than 100`.
func Describe(c store.Condition) string {
	if c.Op == "exists" || c.Op == "missing" {
		return c.Field + " " + OpName(c.Op)
	}
	return c.Field + " " + OpName(c.Op) + " " + c.Value
}

// TopicMatches reports whether an event topic fits a trigger's topic
// ("wishlist.price_dropped", "wishlist.*", "*").
func TopicMatches(pattern, topic string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	for _, p := range strings.Split(pattern, ",") {
		p = strings.TrimSpace(p)
		switch {
		case p == "*":
			return true
		case strings.HasSuffix(p, "*"):
			if strings.HasPrefix(topic, strings.TrimSuffix(p, "*")) {
				return true
			}
		case p == topic:
			return true
		}
	}
	return false
}
