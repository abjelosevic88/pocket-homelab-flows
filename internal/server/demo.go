package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// seedDemo fills the owner's account with flows, two months of events and
// the runs they started (DEMO_DATA=true; deterministic; only into an empty account).
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	if n := s.store.CountsFor(ctx, uid)["flows"]; n > 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	now := time.Now()

	mk := func(key string, enabled bool, edit func(f *store.Flow)) *store.Flow {
		rc, _ := recipeByKey(key)
		f := rc.Flow
		f.Steps = append([]store.Step(nil), rc.Flow.Steps...)
		for i := range f.Steps {
			cfg := map[string]string{}
			for k, v := range f.Steps[i].Config {
				cfg[k] = v
			}
			f.Steps[i].Config = cfg
		}
		f.Recipe, f.Description, f.Enabled = key, rc.Description, enabled
		if edit != nil {
			edit(&f)
		}
		return &f
	}
	custom := func(name, desc, kind string, t store.Trigger, steps ...store.Step) *store.Flow {
		return &store.Flow{Name: name, Description: desc, Enabled: true, TriggerKind: kind, Trigger: t, Steps: steps}
	}
	flows := []*store.Flow{
		mk("wishlist-price-drop", true, nil),
		mk("document-review-task", true, func(f *store.Flow) { f.Steps[0].Config["list_id"] = "2" }),
		mk("invoice-paid-income", true, func(f *store.Flow) { f.Steps[0].Config["account_id"] = "1" }),
		mk("finished-book-note", true, nil),
		mk("morning-ai-quote", true, nil),
		mk("webhook-task", true, func(f *store.Flow) { f.Name = "Shortcut: add a task" }),
		mk("server-alert", true, nil),
		mk("weekly-plan", true, nil),
		custom("Fuel into Money", "Every fill-up logged in Pocket Garage is booked as a fuel expense.", store.TriggerEvent,
			store.Trigger{Topic: "garage.fuel_logged"},
			store.Step{ID: "s1", Kind: "money.add", Config: map[string]string{"kind": "expense", "account_id": "1", "amount": "{{cost}}", "description": "Fuel {{liters}} L — {{vehicle}}", "payee": "{{station}}", "date": "{{date}}"}}),
		custom("Movie night", "Dims the living room through Home Assistant and tells everyone it is starting.", store.TriggerManual, store.Trigger{},
			store.Step{ID: "s1", Kind: "http", Config: map[string]string{"method": "POST", "url": "http://homeassistant.local:8123/api/webhook/movie-night", "headers": "", "body": `{"scene": "cinema"}`}},
			store.Step{ID: "s2", Kind: "notify", Config: map[string]string{"title": "Movie night 🍿", "body": "Lights are down. Starting in five minutes.", "priority": "default"}}),
		custom("Big expenses need a second look", "Any expense over 200 becomes a task to check the receipt.", store.TriggerEvent,
			store.Trigger{Topic: "money.transaction_created", Conditions: []store.Condition{{Field: "kind", Op: "equals", Value: "expense"}, {Field: "amount", Op: "gt", Value: "20000"}}},
			store.Step{ID: "s1", Kind: "ai", Config: map[string]string{"prompt": "In under 12 words, say what this purchase probably was: {{description}} at {{payee}}.", "json": "false"}},
			store.Step{ID: "s2", Kind: "tasks.add", Config: map[string]string{"text": "Check receipt: {{description}} ({{amount | cents}}) — {{steps.s1.text}} tomorrow", "list_id": ""}}),
		custom("Backups to my notes", "A line in the daily note after every nightly backup.", store.TriggerEvent,
			store.Trigger{Topic: "backups.run.finished"},
			store.Step{ID: "s1", Kind: "notes.capture", Config: map[string]string{"text": "Backup {{job}} finished: {{size}} in {{duration}}"}}),
	}
	flows[len(flows)-1].Enabled = false
	for _, f := range flows {
		if msg := s.checkFlow(f); msg != "" {
			return fmt.Errorf("demo flow %q: %s", f.Name, msg)
		}
		f.Source = "app"
		f.NextRunAt = nextRun(f, now)
		if err := s.store.CreateFlow(ctx, uid, f); err != nil {
			return err
		}
	}
	byName := map[string]*store.Flow{}
	for _, f := range flows {
		byName[f.Name] = f
	}

	type stat struct {
		runs, fails int
		last        time.Time
		status      string
	}
	stats := map[int64]*stat{}
	coreID := int64(5000)
	at := func(daysAgo int, hour int) time.Time {
		d := now.AddDate(0, 0, -daysAgo)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, rng.Intn(60), rng.Intn(60), 0, time.Local)
	}
	ts := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

	addRun := func(f *store.Flow, kind, ref, summary string, input map[string]any, when time.Time, fail string) error {
		if when.After(now) {
			return nil
		}
		r := &store.Run{UserID: uid, FlowID: f.ID, FlowName: f.Name, TriggerKind: kind, TriggerRef: ref, Summary: summary, Input: input,
			Outputs: map[string]json.RawMessage{}, Log: []store.LogEntry{}, CreatedAt: ts(when), StartedAt: ts(when.Add(300 * time.Millisecond))}
		v := s.vars(r, f, &store.User{Name: "Maya", Email: "demo@example.com"})
		cur := when.Add(400 * time.Millisecond)
		status := store.RunOK
		for i, st := range f.Steps {
			ms := int64(80 + rng.Intn(400))
			e := store.LogEntry{Step: i + 1, StepID: st.ID, Kind: st.Kind, Name: stepName(st), At: ts(cur), MS: ms, Status: "ok"}
			cfg := flow.RenderAll(st.Config, v)
			out := map[string]any{}
			if st.Kind == "filter" || !flow.CheckAll(st.OnlyIf, st.Match, v) {
				e.Status, e.Message = "skipped", "Skipped"
			}
			if fail != "" && i == len(f.Steps)-1 {
				e.Status, e.Message = "failed", fail
				r.Error = stepName(st) + ": " + fail
				status = store.RunFailed
			} else if e.Status == "ok" {
				switch st.Kind {
				case "notify":
					e.Message = "Sent “" + flow.Preview(cfg["title"], 80) + "”"
				case "tasks.add":
					e.Message = "Task added: " + flow.Preview(cfg["text"], 80)
					out = map[string]any{"id": 300 + rng.Intn(500), "title": cfg["text"]}
				case "notes.capture":
					e.Message = "Added to today's note"
				case "money.add":
					amt, _ := flow.Number(cfg["amount"])
					e.Message = fmt.Sprintf("%s of %.2f recorded", map[string]string{"income": "Income", "expense": "Expense"}[cfg["kind"]], amt)
					out = map[string]any{"id": 9000 + rng.Intn(900)}
				case "ai":
					e.MS = int64(1800 + rng.Intn(2600))
					texts := []string{"Start with the one task you keep moving; the rest gets lighter after it.", "Leave ten minutes between meetings today and use them to stand up.",
						"Write down what you want to have finished by lunch, then protect that time.", "Answer the hard email first, before the easy ones fill the morning."}
					t := texts[rng.Intn(len(texts))]
					if f.Name == "Big expenses need a second look" {
						t = "Probably a household appliance from an electronics shop."
					}
					e.Message = flow.Preview(t, 120)
					out = map[string]any{"text": t, "provider": "pocket"}
				case "http":
					e.Message = "POST homeassistant.local → 200"
					out = map[string]any{"status": 200, "body": "{}"}
				}
			}
			b, _ := json.Marshal(out)
			r.Outputs[st.ID] = b
			v["steps"].(map[string]any)[st.ID] = out
			r.Log = append(r.Log, e)
			cur = cur.Add(time.Duration(e.MS) * time.Millisecond)
			r.Step = i + 1
			if status == store.RunFailed {
				break
			}
		}
		r.Status, r.FinishedAt = status, ts(cur)
		if err := s.store.InsertRun(ctx, r); err != nil {
			return err
		}
		stt := stats[f.ID]
		if stt == nil {
			stt = &stat{}
			stats[f.ID] = stt
		}
		stt.runs++
		if status == store.RunFailed {
			stt.fails++
		}
		if when.After(stt.last) {
			stt.last, stt.status = when, status
		}
		return nil
	}
	event := func(topic string, when time.Time, payload map[string]any, f *store.Flow, fail string) error {
		if when.After(now) {
			return nil
		}
		coreID++
		payload["user_email"] = "demo@example.com"
		app := topic[:indexDot(topic)]
		e := &store.Event{UserID: uid, CoreID: coreID, Topic: topic, Source: app, Subject: fmt.Sprintf("%s:%d", app, rng.Intn(900)+1), Payload: payload, ReceivedAt: ts(when)}
		matched := f != nil && f.Enabled && flow.CheckAll(f.Trigger.Conditions, f.Trigger.Match, flow.Vars{"trigger": payload})
		if matched {
			e.Matched = 1
		}
		if err := s.store.InsertEvent(ctx, e); err != nil {
			return err
		}
		if !matched {
			return nil
		}
		input := map[string]any{}
		for k, v := range payload {
			input[k] = v
		}
		input["event"] = map[string]any{"topic": topic, "app": app, "source": app, "subject": e.Subject, "id": coreID, "at": e.ReceivedAt}
		return addRun(f, store.TriggerEvent, fmt.Sprintf("event:%d", coreID), eventSummary(topic, payload), input, when.Add(time.Second), fail)
	}

	wishes := []struct {
		name, store string
		price       int
	}{{"Sony WH-1000XM5", "Amazon.de", 29900}, {"Le Creuset casserole 24 cm", "Galeria", 21900}, {"Kindle Paperwhite", "Amazon.de", 14999}, {"Trail running shoes", "Bergfreunde", 13995}, {"Moka pot, 6 cups", "Bialetti", 3990}}
	docs := []string{"Electricity bill October", "Car insurance renewal", "Payslip", "Dentist invoice", "Internet bill", "Bank statement", "Warranty — dishwasher", "Tax notice", "Water bill", "Rental contract addendum"}
	clients := []string{"Northwind d.o.o.", "Atelier Brod", "Kestrel Labs", "Sava Design"}
	books := [][2]string{{"Tomorrow, and Tomorrow, and Tomorrow", "Gabrielle Zevin"}, {"The Dervish and Death", "Meša Selimović"}, {"Project Hail Mary", "Andy Weir"}, {"Klara and the Sun", "Kazuo Ishiguro"}, {"Bridge on the Drina", "Ivo Andrić"}}
	payees := []string{"Konzum", "Bingo", "dm", "Shell", "Amko Komerc", "Tehnomag", "Apoteka", "Pekara Bosna"}

	for d := 60; d >= 0; d-- {
		// documents
		if rng.Intn(3) == 0 {
			if err := event("documents.processed", at(d, 9+rng.Intn(9)), map[string]any{"id": 200 + d, "title": docs[rng.Intn(len(docs))], "pages": 1 + rng.Intn(4)}, byName["Check new documents"], ""); err != nil {
				return err
			}
		}
		// price drops
		if rng.Intn(6) == 0 {
			w := wishes[rng.Intn(len(wishes))]
			was := w.price + (1+rng.Intn(6))*1000
			fail := ""
			if d == 23 {
				fail = "notify HTTP 502: Bad Gateway"
			}
			if err := event("wishlist.price_dropped", at(d, 6+rng.Intn(14)), map[string]any{"wish_id": 10 + rng.Intn(20), "wish": w.name, "price": w.price - rng.Intn(5)*500, "was": was, "currency": "EUR", "store": w.store, "url": "https://shop.example/item"}, byName["Wish got cheaper"], fail); err != nil {
				return err
			}
		}
		// spending
		for i := 0; i < 1+rng.Intn(4); i++ {
			amount := (300 + rng.Intn(9000)) * 1
			if rng.Intn(14) == 0 {
				amount = 20000 + rng.Intn(60000)
			}
			if err := event("money.transaction_created", at(d, 8+rng.Intn(13)), map[string]any{"id": 9000 + d*5 + i, "kind": "expense", "amount": amount, "currency": "BAM", "description": []string{"Groceries", "Fuel", "Pharmacy", "Coffee", "Electronics", "Bakery"}[rng.Intn(6)], "payee": payees[rng.Intn(len(payees))], "date": at(d, 12).Format("2006-01-02")}, byName["Big expenses need a second look"], ""); err != nil {
				return err
			}
		}
		// habits and tasks (no flow listens: they only show in Activity)
		if rng.Intn(2) == 0 {
			if err := event("habits.marked", at(d, 21), map[string]any{"habit_id": 3, "habit": []string{"Read", "Walk 8k steps", "No sugar"}[rng.Intn(3)], "date": at(d, 12).Format("2006-01-02"), "status": 1}, nil, ""); err != nil {
				return err
			}
		}
		if rng.Intn(2) == 0 {
			if err := event("tasks.completed", at(d, 10+rng.Intn(9)), map[string]any{"id": 700 + d, "title": []string{"Call the plumber", "Renew passport", "Send the quarterly report", "Book the car service", "Buy a birthday present"}[rng.Intn(5)], "list_id": 2, "status": "COMPLETED"}, nil, ""); err != nil {
				return err
			}
		}
		// invoices, books, fuel, alerts
		if d%11 == 3 {
			c := clients[rng.Intn(len(clients))]
			total := float64(400+rng.Intn(2400)) + 0.5*float64(rng.Intn(2))
			if err := event("invoicing.invoice_paid", at(d, 11), map[string]any{"invoice_id": 40 + d, "number": fmt.Sprintf("2026-%03d", 60-d), "client": c, "total": total, "amount_paid": total, "status": "paid"}, byName["Paid invoices → income"], ""); err != nil {
				return err
			}
		}
		if d%14 == 5 {
			b := books[(d/14)%len(books)]
			if err := event("bookshelf.finished", at(d, 22), map[string]any{"book_id": d, "title": b[0], "authors": b[1], "rating": 4 + rng.Intn(2), "status": "read"}, byName["Finished books in my notes"], ""); err != nil {
				return err
			}
		}
		if d%7 == 2 {
			l := 38 + rng.Intn(12)
			if err := event("garage.fuel_logged", at(d, 17), map[string]any{"vehicle": "Škoda Octavia", "liters": l, "cost": fmt.Sprintf("%.2f", float64(l)*2.61), "station": "Hifa Petrol", "date": at(d, 17).Format("2006-01-02"), "odometer": 128400 + (60-d)*55}, byName["Fuel into Money"], ""); err != nil {
				return err
			}
		}
		if d == 41 || d == 17 || d == 4 {
			sev := "warning"
			if d != 41 {
				sev = "critical"
			}
			if err := event("monitoring.alert", at(d, 3), map[string]any{"target": "nas", "metric": "disk", "severity": sev, "message": "/mnt/tank is 93% full", "value": 93}, byName["Critical server alerts"], ""); err != nil {
				return err
			}
		}
		// schedules
		day := now.AddDate(0, 0, -d)
		wd := int(day.Weekday())
		if wd >= 1 && wd <= 5 {
			f := byName["Morning thought"]
			when := time.Date(day.Year(), day.Month(), day.Day(), 7, 30, 0, 0, time.Local)
			fail := ""
			if d == 12 {
				fail = "notify HTTP 502: Bad Gateway"
			}
			if err := addRun(f, store.TriggerSchedule, "schedule:"+ts(when), flow.DescribeSchedule(f.Trigger), map[string]any{"scheduled_for": when.Format(time.RFC3339)}, when, fail); err != nil {
				return err
			}
		}
		if wd == 0 {
			f := byName["Plan the week"]
			when := time.Date(day.Year(), day.Month(), day.Day(), 18, 0, 0, 0, time.Local)
			if err := addRun(f, store.TriggerSchedule, "schedule:"+ts(when), flow.DescribeSchedule(f.Trigger), map[string]any{"scheduled_for": when.Format(time.RFC3339)}, when, ""); err != nil {
				return err
			}
		}
		// the shortcut webhook
		if rng.Intn(3) == 0 {
			f := byName["Shortcut: add a task"]
			txt := []string{"Buy milk tomorrow", "Call mum on Sunday", "Return the library books Friday", "Water the plants tonight", "Pick up the parcel tomorrow 17h"}[rng.Intn(5)]
			if err := addRun(f, store.TriggerWebhook, fmt.Sprintf("hook:demo%d", d), "Webhook POST", map[string]any{"text": txt, "method": "POST", "query": map[string]any{}}, at(d, 8+rng.Intn(12)), ""); err != nil {
				return err
			}
		}
		if d%9 == 0 && d > 0 {
			f := byName["Movie night"]
			if err := addRun(f, store.TriggerManual, fmt.Sprintf("manual:demo%d", d), "Run by hand", map[string]any{}, at(d, 20), ""); err != nil {
				return err
			}
		}
	}
	ids := make([]int64, 0, len(stats))
	for id := range stats {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		st := stats[id]
		if err := s.store.SetFlowStats(ctx, uid, id, st.runs, st.fails, ts(st.last), st.status); err != nil {
			return err
		}
	}
	s.log.Info("demo data seeded", "flows", len(flows))
	return nil
}

func indexDot(s string) int {
	for i := range s {
		if s[i] == '.' {
			return i
		}
	}
	return len(s)
}
