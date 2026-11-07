package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/config"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://flows.test"
	cfg.MaxBodyBytes = 64 << 20
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func numOf(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }

// waitRun waits until a run is finished (or parked) and returns it.
func (a *app) waitRun(auth string, id int64) map[string]any {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := a.ok(auth, "GET", fmt.Sprintf("/api/v1/runs/%d", id), nil)
		switch r["status"] {
		case "ok", "failed", "stopped", "waiting", "cancelled":
			return r
		}
		time.Sleep(30 * time.Millisecond)
	}
	a.t.Fatalf("run %d did not finish", id)
	return nil
}

// sink is a webhook receiver that records what it got.
type sink struct {
	mu     sync.Mutex
	bodies []string
	status int
}

func newSink(t *testing.T) (*sink, string) {
	sk := &sink{status: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sk.mu.Lock()
		sk.bodies = append(sk.bodies, string(b))
		code := sk.status
		sk.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"ok": true, "n": 7}`))
	}))
	t.Cleanup(srv.Close)
	return sk, srv.URL
}

func (sk *sink) got() []string {
	sk.mu.Lock()
	defer sk.mu.Unlock()
	return append([]string(nil), sk.bodies...)
}

// Every flow, run and event is its owner's: someone else's ids are 404,
// lists never mix people, admins included.
func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	bobFlow := a.ok(a.bob, "POST", "/api/v1/flows", map[string]any{"name": "Bob's flow", "trigger_kind": "manual",
		"steps": []map[string]any{{"kind": "wait", "config": map[string]string{"amount": "1", "unit": "minutes"}}}})
	id := int64(numOf(bobFlow, "id"))
	a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Alice's flow", "trigger_kind": "manual"})

	for _, m := range []string{"GET", "PUT", "DELETE"} {
		a.expect(404, a.alice, m, fmt.Sprintf("/api/v1/flows/%d", id), map[string]any{"name": "x"})
	}
	a.expect(404, a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/run", id), map[string]any{})
	a.expect(404, a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/enabled", id), map[string]any{"enabled": false})
	a.expect(404, a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/duplicate", id), nil)

	run := a.expect(202, a.bob, "POST", fmt.Sprintf("/api/v1/flows/%d/run", id), map[string]any{"input": map[string]any{"x": 1}})
	rid := int64(numOf(run, "id"))
	a.waitRun(a.bob, rid)
	a.expect(404, a.alice, "GET", fmt.Sprintf("/api/v1/runs/%d", rid), nil)
	a.expect(404, a.alice, "POST", fmt.Sprintf("/api/v1/runs/%d/cancel", rid), nil)

	_, _, list := a.call(a.alice, "GET", "/api/v1/flows", nil)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "Alice's flow" {
		t.Fatalf("Alice sees %v", list)
	}
	_, _, runs := a.call(a.alice, "GET", "/api/v1/runs", nil)
	if len(runs) != 0 {
		t.Fatalf("Alice sees Bob's runs: %v", runs)
	}
	// Bob can cancel his own waiting run
	a.ok(a.bob, "POST", fmt.Sprintf("/api/v1/runs/%d/cancel", rid), nil)
}

// Events belong to the person in user_email and only start that person's flows.
func TestEventsStartOnlyTheOwnersFlows(t *testing.T) {
	a := newApp(t, nil)
	_, url := newSink(t)
	mk := func(tok string) int64 {
		f := a.ok(tok, "POST", "/api/v1/flows", map[string]any{"name": "Cheaper", "trigger_kind": "event",
			"trigger": map[string]any{"topic": "wishlist.price_dropped", "conditions": []map[string]string{{"field": "price", "op": "lt", "value": "100"}}},
			"steps":   []map[string]any{{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": `{"wish": "{{wish}}"}`}}}})
		return int64(numOf(f, "id"))
	}
	aliceFlow, bobFlow := mk(a.alice), mk(a.bob)
	ctx := context.Background()
	send := func(id int64, email string, price int) {
		p, _ := json.Marshal(map[string]any{"user_email": email, "wish": "Lamp", "price": price})
		a.s.handleEvent(ctx, core.Event{ID: id, Topic: "wishlist.price_dropped", Source: "wishlist", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	send(1, "alice@example.com", 79)
	send(1, "alice@example.com", 79) // the same event again: nothing new
	send(2, "alice@example.com", 150)
	send(3, "stranger@example.com", 10)

	runs := func(tok string, flow int64) []any {
		_, _, l := a.call(tok, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", flow), nil)
		return l
	}
	if l := runs(a.alice, aliceFlow); len(l) != 1 {
		t.Fatalf("Alice's flow ran %d times, want 1", len(l))
	}
	if l := runs(a.bob, bobFlow); len(l) != 0 {
		t.Fatalf("Bob's flow ran on Alice's event")
	}
	_, _, evs := a.call(a.bob, "GET", "/api/v1/events", nil)
	if len(evs) != 0 {
		t.Fatalf("Bob sees Alice's events")
	}
	_, _, evs = a.call(a.alice, "GET", "/api/v1/events", nil)
	if len(evs) != 2 {
		t.Fatalf("Alice has %d events, want 2", len(evs))
	}
	// Flows' own events never start flows
	p, _ := json.Marshal(map[string]any{"user_email": "alice@example.com", "price": 1})
	a.s.handleEvent(ctx, core.Event{ID: 9, Topic: "flows.run_failed", Source: "flows", Payload: p})
	_, _, evs = a.call(a.alice, "GET", "/api/v1/events", nil)
	if len(evs) != 2 {
		t.Fatal("a Flows event was recorded")
	}
}

// Pocket Forms answers start flows: Flows asks for events:read in Forms, and
// a flow reads the answer's fields and role keys.
func TestFormsAnswerStartsAFlow(t *testing.T) {
	if !strings.Contains(fmt.Sprint(linkUses()), "forms [events:read]") || catalog.TopicName("forms.response_received") == "" {
		t.Fatalf("Forms is not in the catalog: %v", linkUses())
	}
	a := newApp(t, nil)
	sk, url := newSink(t)
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Briefs", "trigger_kind": "event",
		"trigger": map[string]any{"topic": "forms.response_received", "conditions": []map[string]string{{"field": "form_title", "op": "equals", "value": "Client brief"}}},
		"steps":   []map[string]any{{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "{{name}} <{{email}}> {{budget}} {{fields.q2}} {{response_url}}"}}}})
	ctx := context.Background()
	send := func(id int64, title string) {
		p, _ := json.Marshal(map[string]any{"user_email": "alice@example.com", "form_id": 4, "form_title": title, "response_id": 31, "number": 12,
			"name": "Marko", "email": "marko@example.com", "budget": "5000", "answers": map[string]any{"Budget": "5000", "What do you need?": "A shop"},
			"fields": map[string]any{"q1": "5000", "q2": "A shop"}, "response_url": "https://forms.example/answers/31"})
		a.s.handleEvent(ctx, core.Event{ID: id, Topic: "forms.response_received", Source: "forms", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	send(1, "Client brief")
	send(2, "Feedback")
	_, _, l := a.call(a.alice, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", int64(numOf(f, "id"))), nil)
	if len(l) != 1 {
		t.Fatalf("the flow ran %d times, want 1", len(l))
	}
	a.waitRun(a.alice, int64(numOf(l[0].(map[string]any), "id")))
	if got := sk.got(); len(got) != 1 || got[0] != "Marko <marko@example.com> 5000 A shop https://forms.example/answers/31" {
		t.Fatalf("got %q", got)
	}
	if r := a.ok(a.alice, "POST", "/api/v1/recipes/form-answer-push", nil); r["enabled"] != false {
		t.Fatalf("recipe: %v", r)
	}
}

// Pocket CRM deals start flows: Flows asks for events:read in CRM, a new
// flow knows a deal's fields before the first event, and a won deal fills in
// its title, amount and currency.
func TestCRMDealWonStartsAFlow(t *testing.T) {
	if !strings.Contains(fmt.Sprint(linkUses()), "crm [events:read]") || catalog.TopicName("crm.deal_won") == "" || catalog.TopicName("crm.steps_due") == "" {
		t.Fatalf("CRM is not in the catalog: %v", linkUses())
	}
	a := newApp(t, nil)
	fl := a.ok(a.alice, "GET", "/api/v1/flows/fields?kind=event&topic=crm.deal_moved", nil)
	if got := fmt.Sprint(fl["fields"]); !strings.Contains(got, "from_stage") || !strings.Contains(got, "amount") || !strings.Contains(got, "event.topic") {
		t.Fatalf("fields before the first event: %v", got)
	}
	sk, url := newSink(t)
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Won", "trigger_kind": "event",
		"trigger": map[string]any{"topic": "crm.deal_won"},
		"steps":   []map[string]any{{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "{{title}} {{amount}} {{currency}} {{value | cents}} {{url}}"}}}})
	p, _ := json.Marshal(map[string]any{"user_email": "alice@example.com", "deal_id": 7, "title": "Shop for Marko", "stage": "Won", "status": "won",
		"value": 450000, "amount": 4500, "currency": "EUR", "company": "Marko d.o.o.", "url": "https://crm.example/deals/7"})
	a.s.handleEvent(context.Background(), core.Event{ID: 1, Topic: "crm.deal_won", Source: "crm", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	_, _, l := a.call(a.alice, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", int64(numOf(f, "id"))), nil)
	if len(l) != 1 {
		t.Fatalf("the flow ran %d times, want 1", len(l))
	}
	a.waitRun(a.alice, int64(numOf(l[0].(map[string]any), "id")))
	if got := sk.got(); len(got) != 1 || got[0] != "Shop for Marko 4500 EUR 4500.00 https://crm.example/deals/7" {
		t.Fatalf("got %q", got)
	}
	for _, key := range []string{"deal-won-push", "deal-steps-push"} {
		if r := a.ok(a.alice, "POST", "/api/v1/recipes/"+key, nil); r["enabled"] != false {
			t.Fatalf("recipe %s: %v", key, r)
		}
	}
}

// Pocket League games start flows: Flows asks for events:read in League, a
// new flow knows a game's fields before the first event, and a game short of
// players fills in the group, the counts and the time.
func TestLeagueShortStartsAFlow(t *testing.T) {
	if !strings.Contains(fmt.Sprint(linkUses()), "league [events:read]") || catalog.TopicName("league.short") == "" || catalog.TopicName("league.game_planned") == "" {
		t.Fatalf("League is not in the catalog: %v", linkUses())
	}
	a := newApp(t, nil)
	fl := a.ok(a.alice, "GET", "/api/v1/flows/fields?kind=event&topic=league.game_planned", nil)
	if got := fmt.Sprint(fl["fields"]); !strings.Contains(got, "venue") || !strings.Contains(got, "starts_at") || !strings.Contains(got, "event.topic") {
		t.Fatalf("fields before the first event: %v", got)
	}
	sk, url := newSink(t)
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Short", "trigger_kind": "event",
		"trigger": map[string]any{"topic": "league.short"},
		"steps":   []map[string]any{{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "{{group}} {{in}}/{{needed}} {{starts_at | date: DD.MM. HH:mm}} {{url}}"}}}})
	p, _ := json.Marshal(map[string]any{"user_email": "alice@example.com", "game_id": 31, "group": "Tuesday Tennis", "starts_at": "2026-11-10T20:00",
		"in": 3, "needed": 4, "url": "https://league.example/games/31"})
	a.s.handleEvent(context.Background(), core.Event{ID: 1, Topic: "league.short", Source: "league", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	_, _, l := a.call(a.alice, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", int64(numOf(f, "id"))), nil)
	if len(l) != 1 {
		t.Fatalf("the flow ran %d times, want 1", len(l))
	}
	a.waitRun(a.alice, int64(numOf(l[0].(map[string]any), "id")))
	if got := sk.got(); len(got) != 1 || got[0] != "Tuesday Tennis 3/4 10.11. 20:00 https://league.example/games/31" {
		t.Fatalf("got %q", got)
	}
	for _, key := range []string{"league-short-push", "league-planned-push"} {
		if r := a.ok(a.alice, "POST", "/api/v1/recipes/"+key, nil); r["enabled"] != false {
			t.Fatalf("recipe %s: %v", key, r)
		}
	}
}

// Pocket Tables rows start flows and flows add rows: Flows asks for
// events:read, tables:write and tables:read in Tables, a new flow knows a
// row's fields before the first event, conditions read the table, the
// changed fields and a value by field name, and "Add a row to a table" picks
// the table from the bases and sends the values by field name.
func TestTablesRowStartsAFlowAndAddsARow(t *testing.T) {
	if !strings.Contains(fmt.Sprint(linkUses()), "tables [events:read tables:write tables:read]") || catalog.TopicName("tables.record_updated") == "" || catalog.TopicName("tables.reminder") == "" {
		t.Fatalf("Tables is not in the catalog: %v", linkUses())
	}
	var mu sync.Mutex
	var got []map[string]any
	tables := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/bases":
			_, _ = w.Write([]byte(`[{"id": 1, "name": "Home", "tables": [{"id": 4, "name": "Bookings"}, {"id": 5, "name": "Guests"}]}, {"id": 2, "name": "Empty", "tables": []}]`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/tables/5/records":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			got = append(got, in)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 77, "table_id": 5, "number": 12, "title": "Marko", "values": {}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(tables.Close)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"connections": map[string]any{"tables": map[string]any{"mode": "direct", "url": tables.URL, "token": "pt_tables_token_1"}}})
	pk := a.ok(a.alice, "GET", "/api/v1/pickers?name=tables.tables", nil)
	if o := fmt.Sprint(pk["options"]); pk["ok"] != true || !strings.Contains(o, "map[label:Home · Bookings value:4]") || !strings.Contains(o, "map[label:Home · Guests value:5]") || strings.Contains(o, "Empty") {
		t.Fatalf("tables to pick from: %v", pk)
	}
	fl := a.ok(a.alice, "GET", "/api/v1/flows/fields?kind=event&topic=tables.record_updated", nil)
	if got := fmt.Sprint(fl["fields"]); !strings.Contains(got, "changed") || !strings.Contains(got, "values") || !strings.Contains(got, "table") || !strings.Contains(got, "event.topic") {
		t.Fatalf("fields before the first event: %v", got)
	}
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Confirmed bookings to guests", "trigger_kind": "event",
		"trigger": map[string]any{"topic": "tables.record_updated", "conditions": []map[string]string{
			{"field": "table", "op": "equals", "value": "Bookings"}, {"field": "changed", "op": "contains", "value": "Status"}, {"field": "values.Status", "op": "equals", "value": "Confirmed"}}},
		"steps": []map[string]any{{"id": "s1", "kind": "tables.add", "config": map[string]string{"table_id": "5",
			"values": "Name: {{values.Guest}}\nNotes: {{values.Notes}}\nArrives: {{values.Check-in}}\nEmpty: {{values.Nothing}}\nno colon here"}}}})
	ev := func(id int64, table, status string) {
		p, _ := json.Marshal(map[string]any{"user_email": "alice@example.com", "table_id": 4, "table": table, "base": "Home", "record_id": 9, "title": "Marko", "number": 3,
			"values":  map[string]any{"Guest": "Marko", "Status": status, "Check-in": "2026-11-20", "Notes": "late\narrival"},
			"changed": []string{"Status"}, "changes": []map[string]string{{"field": "Status", "before": "New", "after": status}}, "url": "https://tables.example/r/9"})
		a.s.handleEvent(context.Background(), core.Event{ID: id, Topic: "tables.record_updated", Source: "tables", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	ev(1, "Bookings", "New")
	ev(2, "Guests", "Confirmed")
	ev(3, "Bookings", "Confirmed")
	_, _, l := a.call(a.alice, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", int64(numOf(f, "id"))), nil)
	if len(l) != 1 {
		t.Fatalf("the flow ran %d times, want 1", len(l))
	}
	run := a.waitRun(a.alice, int64(numOf(l[0].(map[string]any), "id")))
	if run["status"] != "ok" {
		t.Fatalf("run: %v", run)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("rows added: %v", got)
	}
	body := got[0]
	if fmt.Sprint(body["values"]) != "map[Arrives:2026-11-20 Name:Marko Notes:late\narrival]" || body["typecast"] != true || !strings.HasPrefix(fmt.Sprint(body["ref"]), "pocket-flows:") {
		t.Fatalf("row sent: %#v", body)
	}
	if out := fmt.Sprint(run); !strings.Contains(out, tables.URL+"/r/77") {
		t.Fatalf("the step's output has no link to the row: %v", out)
	}
	for _, key := range []string{"tables-row-push", "tables-reminder-push"} {
		if r := a.ok(a.alice, "POST", "/api/v1/recipes/"+key, nil); r["enabled"] != false {
			t.Fatalf("recipe %s: %v", key, r)
		}
	}
}

// Pocket Rentals starts flows and flows report repairs there: Flows asks for
// events:read, rentals:write and rentals:read in Rentals, a new flow knows a
// lease's and a late rent's fields before the first event, a late rent fills
// in what is owed, and "Report a repair in Pocket Rentals" picks the place
// and sends the repair (a form answer here).
func TestRentalsStartsAFlowAndReportsARepair(t *testing.T) {
	if !strings.Contains(fmt.Sprint(linkUses()), "rentals [events:read rentals:write rentals:read]") || catalog.TopicName("rentals.rent_late") == "" || catalog.TopicName("rentals.request_done") == "" {
		t.Fatalf("Rentals is not in the catalog: %v", linkUses())
	}
	for _, topic := range []string{"rentals.rent_due", "rentals.rent_late", "rentals.payment_recorded", "rentals.lease_started", "rentals.lease_ending", "rentals.lease_ended", "rentals.request_created", "rentals.request_done"} {
		if catalog.TopicName(topic) == "" || catalog.TopicFields(topic) == nil {
			t.Fatalf("%s: no name or fields", topic)
		}
	}
	var mu sync.Mutex
	var got []map[string]any
	rentals := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/properties":
			_, _ = w.Write([]byte(`[{"id": 3, "name": "Linden Court"}, {"id": 4, "name": "Old flat", "archived": true}]`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/requests":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			got = append(got, in)
			again := len(got) > 1 && got[0]["ref"] == in["ref"]
			mu.Unlock()
			if again {
				// the same ref twice: the repair it already made
				_, _ = w.Write([]byte(`{"request": {"id": 21, "property_id": 3, "title": "Boiler is off", "priority": "urgent"}, "already": true}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"request": {"id": 21, "property_id": 3, "title": "Boiler is off", "priority": "urgent"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rentals.Close)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"connections": map[string]any{"rentals": map[string]any{"mode": "direct", "url": rentals.URL, "token": "pt_rentals_token_1"}}})
	pk := a.ok(a.alice, "GET", "/api/v1/pickers?name=rentals.places", nil)
	if o := fmt.Sprint(pk["options"]); pk["ok"] != true || !strings.Contains(o, "map[label:Linden Court value:3]") || strings.Contains(o, "Old flat") {
		t.Fatalf("places to pick from: %v", pk)
	}
	fl := a.ok(a.alice, "GET", "/api/v1/flows/fields?kind=event&topic=rentals.lease_ending", nil)
	if got := fmt.Sprint(fl["fields"]); !strings.Contains(got, "days_left") || !strings.Contains(got, "tenants") || !strings.Contains(got, "place") || !strings.Contains(got, "event.topic") {
		t.Fatalf("fields before the first event: %v", got)
	}

	// a late rent fills in who owes how much
	sk, url := newSink(t)
	late := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Late", "trigger_kind": "event",
		"trigger": map[string]any{"topic": "rentals.rent_late"},
		"steps":   []map[string]any{{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "{{place}}: {{tenants}} owes {{owed | cents}} {{currency}} since {{due | date: DD.MM.}}"}}}})
	p, _ := json.Marshal(map[string]any{"user_email": "alice@example.com", "lease_id": 5, "place": "Flat 2 · Linden Court", "tenants": "Ana Petrović",
		"title": "Rent November", "due": "2026-11-01", "owed": 45000, "currency": "EUR", "url": "https://rentals.example/leases/5"})
	a.s.handleEvent(context.Background(), core.Event{ID: 1, Topic: "rentals.rent_late", Source: "rentals", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	_, _, l := a.call(a.alice, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", int64(numOf(late, "id"))), nil)
	if len(l) != 1 {
		t.Fatalf("the late rent flow ran %d times, want 1", len(l))
	}
	a.waitRun(a.alice, int64(numOf(l[0].(map[string]any), "id")))
	if got := sk.got(); len(got) != 1 || got[0] != "Flat 2 · Linden Court: Ana Petrović owes 450.00 EUR since 01.11." {
		t.Fatalf("got %q", got)
	}

	// a form answer reports a repair
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Repairs from a form", "trigger_kind": "event",
		"trigger": map[string]any{"topic": "forms.response_received"},
		"steps": []map[string]any{{"id": "s1", "kind": "rentals.repair", "config": map[string]string{"property_id": "3", "priority": "urgent",
			"title": "  {{preview}}  ", "details": "{{preview}}\n{{email}}", "reported_by": "{{name}}"}}}})
	p, _ = json.Marshal(map[string]any{"user_email": "alice@example.com", "form_title": "Report a repair", "number": 4, "name": "Ana", "email": "ana@example.com", "preview": "Boiler is off"})
	a.s.handleEvent(context.Background(), core.Event{ID: 2, Topic: "forms.response_received", Source: "forms", Payload: p, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	_, _, l = a.call(a.alice, "GET", fmt.Sprintf("/api/v1/runs?flow=%d", int64(numOf(f, "id"))), nil)
	if len(l) != 1 {
		t.Fatalf("the repair flow ran %d times, want 1", len(l))
	}
	run := a.waitRun(a.alice, int64(numOf(l[0].(map[string]any), "id")))
	if run["status"] != "ok" {
		t.Fatalf("run: %v", run)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("repairs reported: %v", got)
	}
	if b := got[0]; numOf(b, "property_id") != 3 || b["title"] != "Boiler is off" || b["priority"] != "urgent" || b["details"] != "Boiler is off\nana@example.com" || b["reported_by"] != "Ana" ||
		b["ref"] != fmt.Sprintf("pocket-flows:%d:s1", int64(numOf(run, "id"))) {
		t.Fatalf("repair sent: %#v", b)
	}
	if out := fmt.Sprint(run); !strings.Contains(out, rentals.URL+"/repairs/21") {
		t.Fatalf("the step's output has no link to the repair: %v", out)
	}
	mu.Unlock()

	// a retry sends the same ref; Rentals answers 200 with the repair it
	// already made, and the step still succeeds with its link
	cfg := map[string]string{"property_id": "3", "priority": "urgent", "title": "Boiler is off"}
	step := store.Step{ID: "s1", Kind: "rentals.repair", Config: cfg}
	sib := &siblingConn{client: pocket.New(rentals.URL, "pt_rentals_token_1", 5*time.Second), public: rentals.URL, via: modeDirect}
	out, line, err := a.s.stepApp(context.Background(), sib, "rentals.repair", cfg, flow.Vars{}, &store.Run{ID: int64(numOf(run, "id"))}, step, nil)
	if err != nil || flow.Text(out["url"]) != rentals.URL+"/repairs/21" || !strings.Contains(line, "Repair reported") {
		t.Fatalf("retry: %v %q %v", out, line, err)
	}
	mu.Lock()
	if len(got) != 2 || got[1]["ref"] != got[0]["ref"] {
		t.Fatalf("the retry sent another ref: %v", got)
	}
	for _, key := range []string{"rent-late-push", "lease-ending-task", "urgent-repair-push"} {
		if r := a.ok(a.alice, "POST", "/api/v1/recipes/"+key, nil); r["enabled"] != false {
			t.Fatalf("recipe %s: %v", key, r)
		}
	}
}

// Steps run in order, fill in placeholders, filters stop quietly, and a
// failing address is retried later instead of failing at once.
func TestEngine(t *testing.T) {
	a := newApp(t, nil)
	sk, url := newSink(t)
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Chain", "trigger_kind": "manual", "steps": []map[string]any{
		{"id": "call", "kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": `{"hello": "{{who}}"}`}},
		{"kind": "filter", "only_if": []map[string]string{{"field": "steps.call.json.n", "op": "gt", "value": "5"}}},
		{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "n was {{steps.call.json.n}} for {{me.name}}"}},
		{"kind": "filter", "only_if": []map[string]string{{"field": "who", "op": "equals", "value": "nobody"}}},
		{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "never"}},
	}})
	id := int64(numOf(f, "id"))
	run := a.expect(202, a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/run", id), map[string]any{"input": map[string]any{"who": "world"}})
	r := a.waitRun(a.alice, int64(numOf(run, "id")))
	if r["status"] != "stopped" {
		t.Fatalf("status %v, want stopped (%v)", r["status"], r["log"])
	}
	got := sk.got()
	if len(got) != 2 || got[0] != `{"hello": "world"}` || got[1] != "n was 7 for Alice" {
		t.Fatalf("calls %q", got)
	}

	// a 5xx is retried later: the run waits with a note in the log
	sk.mu.Lock()
	sk.status = 503
	sk.mu.Unlock()
	run = a.expect(202, a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/run", id), map[string]any{"input": map[string]any{"who": "x"}})
	r = a.waitRun(a.alice, int64(numOf(run, "id")))
	if r["status"] != "waiting" || numOf(r, "attempt") != 1 {
		t.Fatalf("after a 503: %v attempt %v", r["status"], r["attempt"])
	}
	// a 4xx fails at once, and Retry starts it again from that step
	sk.mu.Lock()
	sk.status = 400
	sk.mu.Unlock()
	run = a.expect(202, a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/run", id), map[string]any{"input": map[string]any{"who": "y"}})
	rid := int64(numOf(run, "id"))
	r = a.waitRun(a.alice, rid)
	if r["status"] != "failed" || !strings.Contains(r["error"].(string), "400") {
		t.Fatalf("after a 400: %v %v", r["status"], r["error"])
	}
	sk.mu.Lock()
	sk.status = 200
	sk.mu.Unlock()
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/runs/%d/retry", rid), nil)
	if r = a.waitRun(a.alice, rid); r["status"] != "stopped" {
		t.Fatalf("retried run: %v", r["status"])
	}
	flow := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/flows/%d", id), nil)
	if numOf(flow, "run_count") < 2 || numOf(flow, "fail_count") != 1 {
		t.Fatalf("counts %v / %v", flow["run_count"], flow["fail_count"])
	}
}

// A webhook flow starts from its secret address, with the body as input.
func TestWebhook(t *testing.T) {
	a := newApp(t, nil)
	sk, url := newSink(t)
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Hook", "trigger_kind": "webhook",
		"trigger": map[string]any{"conditions": []map[string]string{{"field": "text", "op": "exists"}}},
		"steps":   []map[string]any{{"kind": "http", "config": map[string]string{"method": "POST", "url": url, "body": "{{text}} / {{q}}"}}}})
	hook := f["hook_url"].(string)
	path := strings.TrimPrefix(hook, "http://flows.test")
	if !strings.HasPrefix(path, "/hooks/") {
		t.Fatalf("hook url %q", hook)
	}
	m := a.expect(202, "", "POST", path+"?q=hi", map[string]any{"text": "Buy milk"})
	a.waitRun(a.alice, int64(numOf(m, "run_id")))
	if got := sk.got(); len(got) != 1 || got[0] != "Buy milk / hi" {
		t.Fatalf("got %q", got)
	}
	a.expect(200, "", "POST", path, map[string]any{"other": 1}) // conditions not met: nothing starts
	a.expect(404, "", "POST", "/hooks/nope", nil)
	// a new address: the old one stops working
	nf := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/hook", int64(numOf(f, "id"))), nil)
	if nf["hook_url"] == hook {
		t.Fatal("the address did not change")
	}
	a.expect(404, "", "POST", path, map[string]any{"text": "x"})
}

// Members cannot reach the local network from "Call a web address" unless an admin allows it.
func TestHTTPPrivateAddresses(t *testing.T) {
	a := newApp(t, nil)
	_, url := newSink(t) // 127.0.0.1
	mk := func(tok string) int64 {
		f := a.ok(tok, "POST", "/api/v1/flows", map[string]any{"name": "Local", "trigger_kind": "manual", "steps": []map[string]any{{"kind": "http", "config": map[string]string{"method": "GET", "url": url}}}})
		return int64(numOf(f, "id"))
	}
	runOf := func(tok string, id int64) map[string]any {
		r := a.expect(202, tok, "POST", fmt.Sprintf("/api/v1/flows/%d/run", id), map[string]any{})
		return a.waitRun(tok, int64(numOf(r, "id")))
	}
	if r := runOf(a.alice, mk(a.alice)); r["status"] != "ok" {
		t.Fatalf("admin: %v %v", r["status"], r["error"])
	}
	bobFlow := mk(a.bob)
	if r := runOf(a.bob, bobFlow); r["status"] != "failed" || !strings.Contains(r["error"].(string), "local network") {
		t.Fatalf("member: %v %v", r["status"], r["error"])
	}
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"http_private": true}) // not an admin: ignored
	if r := runOf(a.bob, bobFlow); r["status"] != "failed" {
		t.Fatal("a member switched the local network on")
	}
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"http_private": true})
	if r := runOf(a.bob, bobFlow); r["status"] != "ok" {
		t.Fatalf("allowed member: %v %v", r["status"], r["error"])
	}
}

// Settings are per person: connections, notifications.
func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"notify_url": "http://apprise/notify/a", "connections": map[string]any{"tasks": map[string]any{"mode": "direct", "url": "http://tasks:8080", "token": "pt_secret_token_123"}}})
	bob := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if bob["notify_url"] != "" {
		t.Fatal("Bob sees Alice's notification address")
	}
	for _, it := range bob["apps"].([]any) {
		m := it.(map[string]any)
		if m["slug"] == "tasks" && (m["url"] != "" || m["token"] != "") {
			t.Fatalf("Bob sees Alice's Tasks connection: %v", m)
		}
	}
	alice := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	for _, it := range alice["apps"].([]any) {
		m := it.(map[string]any)
		if m["slug"] == "tasks" && (m["mode"] != "direct" || strings.Contains(m["token"].(string), "secret")) {
			t.Fatalf("Alice's Tasks connection: %v", m)
		}
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"connections": map[string]any{"wishlist": map[string]any{"mode": "direct"}}})
}

// Export, then import twice: the second import adds nothing.
func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "POST", "/api/v1/recipes/wishlist-price-drop", nil)
	a.ok(a.alice, "POST", "/api/v1/recipes/webhook-task", nil)
	req, _ := http.NewRequest("GET", a.url+"/api/v1/export.json", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.NewDecoder(res.Body).Decode(&doc)
	res.Body.Close()
	if strings.Contains(fmt.Sprint(doc), "/hooks/") {
		t.Fatal("the export holds a webhook address")
	}
	first := a.ok(a.bob, "POST", "/api/v1/import", doc)
	second := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if numOf(first, "flows") != 2 || numOf(second, "flows") != 0 || numOf(second, "existing") != 2 {
		t.Fatalf("first %v, second %v", first, second)
	}
	_, _, l := a.call(a.bob, "GET", "/api/v1/flows", nil)
	for _, it := range l {
		if it.(map[string]any)["enabled"] != false {
			t.Fatal("an imported flow is switched on")
		}
	}
}

// The n8n import keeps the schedule and the HTTP request and names the rest.
func TestImportN8N(t *testing.T) {
	a := newApp(t, nil)
	wf := map[string]any{"id": "abc", "name": "Nightly ping", "nodes": []map[string]any{
		{"name": "Schedule", "type": "n8n-nodes-base.scheduleTrigger", "parameters": map[string]any{"rule": map[string]any{"interval": []map[string]any{{"field": "days", "triggerAtHour": 2, "triggerAtMinute": 30}}}}},
		{"name": "Ping", "type": "n8n-nodes-base.httpRequest", "parameters": map[string]any{"method": "POST", "url": "https://example.com/{{x}}", "jsonBody": `={"t": "{{ $json.title }}"}`}},
		{"name": "Code", "type": "n8n-nodes-base.code", "parameters": map[string]any{}},
	}, "connections": map[string]any{"Schedule": map[string]any{"main": [][]map[string]any{{{"node": "Ping"}}}}, "Ping": map[string]any{"main": [][]map[string]any{{{"node": "Code"}}}}}}
	out := a.ok(a.alice, "POST", "/api/v1/import/n8n", wf)
	res := out["results"].([]any)[0].(map[string]any)
	if res["status"] != "created" {
		t.Fatalf("%v", res)
	}
	f := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/flows/%d", int64(numOf(res, "flow_id"))), nil)
	tr := f["trigger"].(map[string]any)
	steps := f["steps"].([]any)
	if f["trigger_kind"] != "schedule" || tr["repeat"] != "daily" || tr["time"] != "02:30" || len(steps) != 1 || f["enabled"] != false {
		t.Fatalf("flow %v", f)
	}
	if body := steps[0].(map[string]any)["config"].(map[string]any)["body"]; body != `{"t": "{{ title }}"}` {
		t.Fatalf("body %q", body)
	}
	if !strings.Contains(f["description"].(string), "Code") {
		t.Fatalf("description %q", f["description"])
	}
	again := a.ok(a.alice, "POST", "/api/v1/import/n8n", wf)
	if again["results"].([]any)[0].(map[string]any)["status"] != "exists" {
		t.Fatal("imported twice")
	}
}

// Link tokens only reach their scope's routes.
func TestLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }
	cases := []struct {
		scopes []string
		r      *http.Request
		want   bool
	}{
		{nil, req("GET", "/api/v1/auth/me"), true},
		{[]string{"flows:read"}, req("GET", "/api/v1/flows"), true},
		{[]string{"flows:read"}, req("GET", "/api/v1/runs/4"), true},
		{[]string{"flows:read"}, req("POST", "/api/v1/flows/4/run"), false},
		{[]string{"flows:run"}, req("POST", "/api/v1/flows/4/run"), true},
		{[]string{"flows:read", "flows:run"}, req("PUT", "/api/v1/flows/4"), false},
		{[]string{"flows:read", "flows:run"}, req("POST", "/api/v1/flows"), false},
		{[]string{"flows:read"}, req("GET", "/api/v1/settings"), false},
		{[]string{"flows:read"}, req("GET", "/api/v1/export.json"), false},
	}
	for _, c := range cases {
		if got := linkAllows(c.scopes, c.r); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Schedules get their next time on save and none when switched off.
func TestScheduleNextRun(t *testing.T) {
	a := newApp(t, nil)
	f := a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Daily", "trigger_kind": "schedule", "trigger": map[string]any{"repeat": "daily", "time": "06:15"}})
	if f["next_run_at"] == "" {
		t.Fatal("no next run")
	}
	off := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/flows/%d/enabled", int64(numOf(f, "id"))), map[string]any{"enabled": false})
	if off["next_run_at"] != "" {
		t.Fatal("a switched-off schedule still has a next run")
	}
	a.expect(400, a.alice, "POST", "/api/v1/flows", map[string]any{"trigger_kind": "schedule", "trigger": map[string]any{"repeat": "minutes", "every": 1}})
	a.expect(400, a.alice, "POST", "/api/v1/flows", map[string]any{"trigger_kind": "event"})
	a.expect(400, a.alice, "POST", "/api/v1/flows", map[string]any{"trigger_kind": "manual", "steps": []map[string]any{{"kind": "rm -rf"}}})
}
