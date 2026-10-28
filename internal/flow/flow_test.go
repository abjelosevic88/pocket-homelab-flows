package flow

import (
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

func vars() Vars {
	return Vars{
		"trigger": map[string]any{"title": "Lamp", "price": 79.5, "old_price": "99,00", "tags": []any{"home", "light"},
			"items": []any{map[string]any{"name": "bulb"}}, "at": "2026-10-28T09:15:00Z"},
		"steps": map[string]any{"s1": map[string]any{"text": "Hello\nworld"}},
		"me":    map[string]any{"name": "Maya"},
	}
}

func TestRender(t *testing.T) {
	v := vars()
	cases := map[string]string{
		"{{title}} now {{price}}":           "Lamp now 79.5",
		"{{ trigger.title | upper }}":       "LAMP",
		"{{missing | default: none}}":       "none",
		"{{tags}}":                          "home, light",
		"{{items.0.name}}":                  "bulb",
		"{{steps.s1.text | first_line}}":    "Hello",
		"{{me.name}}":                       "Maya",
		"{{at | date: DD.MM.YYYY}}":         "28.10.2026",
		"{{price | round: 0}}":              "80",
		"no placeholders":                   "no placeholders",
		"{{title | truncate: 2}}":           "La…",
		"{{1250 | cents}}":                  "",
		"{{steps.s1.text | count}} letters": "11 letters",
	}
	for in, want := range cases {
		if got := Render(in, v); got != want {
			t.Errorf("Render(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConditions(t *testing.T) {
	v := vars()
	yes := []store.Condition{
		{Field: "price", Op: "lt", Value: "100"},
		{Field: "old_price", Op: "gt", Value: "98.5"},
		{Field: "title", Op: "equals", Value: "lamp"},
		{Field: "title", Op: "contains", Value: "AM"},
		{Field: "tags", Op: "contains", Value: "light"},
		{Field: "nothing", Op: "missing"},
		{Field: "title", Op: "exists"},
		{Field: "title", Op: "matches", Value: "^l.m"},
		{Field: "price", Op: "equals", Value: "79.50"},
		{Field: "title", Op: "equals", Value: "{{trigger.title}}"},
	}
	for _, c := range yes {
		if !Check(c, v) {
			t.Errorf("%s should hold", Describe(c))
		}
	}
	no := []store.Condition{
		{Field: "price", Op: "gt", Value: "100"},
		{Field: "title", Op: "not_equals", Value: "Lamp"},
		{Field: "title", Op: "lt", Value: "abc"},
		{Field: "nothing", Op: "exists"},
	}
	for _, c := range no {
		if Check(c, v) {
			t.Errorf("%s should not hold", Describe(c))
		}
	}
	if !CheckAll(nil, "", v) || CheckAll(no, "any", v) || !CheckAll(append(no, yes[0]), "any", v) || CheckAll(append(yes, no[0]), "all", v) {
		t.Error("CheckAll")
	}
}

func TestTopicMatches(t *testing.T) {
	if !TopicMatches("wishlist.*", "wishlist.price_dropped") || !TopicMatches("a.b, wishlist.price_dropped", "wishlist.price_dropped") || TopicMatches("wishlist.bought", "wishlist.price_dropped") || TopicMatches("", "x") {
		t.Error("topics")
	}
}

func TestNext(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	at := time.Date(2026, 10, 28, 9, 7, 0, 0, loc) // a Wednesday
	check := func(tr store.Trigger, want string) {
		t.Helper()
		if err := CheckSchedule(&tr); err != nil {
			t.Fatalf("%+v: %v", tr, err)
		}
		got, ok := Next(tr, at, loc)
		if !ok || got.Format("2006-01-02 15:04") != want {
			t.Errorf("%s: got %v %v, want %s", DescribeSchedule(tr), got, ok, want)
		}
	}
	check(store.Trigger{Repeat: "minutes", Every: 15}, "2026-10-28 09:15")
	check(store.Trigger{Repeat: "hourly", Every: 1, Time: "00:30"}, "2026-10-28 09:30")
	check(store.Trigger{Repeat: "hourly", Every: 6, Time: "00:00"}, "2026-10-28 12:00")
	check(store.Trigger{Repeat: "daily", Time: "08:00"}, "2026-10-29 08:00")
	check(store.Trigger{Repeat: "daily", Time: "21:00"}, "2026-10-28 21:00")
	check(store.Trigger{Repeat: "weekly", Days: "15", Time: "07:30"}, "2026-10-30 07:30")
	check(store.Trigger{Repeat: "weekly", Days: "3", Time: "09:00"}, "2026-11-04 09:00")
	check(store.Trigger{Repeat: "monthly", DayOfMonth: 31, Time: "10:00"}, "2026-10-31 10:00")
	check(store.Trigger{Repeat: "monthly", DayOfMonth: 1, Time: "10:00"}, "2026-11-01 10:00")
	tr := store.Trigger{Repeat: "once", At: "2026-10-27T10:00"}
	if _, ok := Next(tr, at, loc); ok {
		t.Error("a past once should not run")
	}
	if err := CheckSchedule(&store.Trigger{Repeat: "minutes", Every: 1}); err == nil {
		t.Error("every minute is too often")
	}
}

func TestFields(t *testing.T) {
	got := Fields(map[string]any{"b": 1, "a": map[string]any{"c": "x"}, "l": []any{map[string]any{"n": 1}}}, 3)
	want := []string{"a.c", "b", "l", "l.0.n"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
