package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// ---- events from Pocket Core ----
//
// Flows follows the bus over SSE with its app token. Core only sends what
// Flows may read: its own events, and other apps' events of a person who
// has the Flows → <app> events:read link switched on. The last event id is
// kept, so after a restart the stream resumes where it was (events older
// than six hours are recorded but start nothing).

const cursorKey = "events:cursor"

// maxEventAge is how old a replayed event may be and still start flows.
const maxEventAge = 6 * time.Hour

func (s *Server) setStream(ok bool, err string) {
	s.streamMu.Lock()
	s.streamOK, s.streamErr = ok, err
	s.streamMu.Unlock()
}

func (s *Server) streamStatus() map[string]any {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	out := map[string]any{"connected": s.streamOK, "error": s.streamErr}
	if !s.lastEvent.IsZero() {
		out["last_event_at"] = s.lastEvent.UTC().Format(time.RFC3339)
	}
	return out
}

// listen follows the bus until ctx ends, reconnecting with backoff.
func (s *Server) listen(ctx context.Context) {
	wait := 2 * time.Second
	for ctx.Err() == nil {
		if s.core.Token() == "" { // not registered yet
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		cursor := s.cursor(ctx)
		err := s.streamOnce(ctx, cursor)
		if ctx.Err() != nil {
			return
		}
		msg := "the stream ended"
		if err != nil {
			msg = err.Error()
		}
		s.setStream(false, msg)
		s.log.Debug("pocket core: event stream closed, reconnecting", "err", msg, "in", wait.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait < time.Minute {
			wait *= 2
		}
		if err == nil {
			wait = 2 * time.Second
		}
	}
}

// cursor is the last event id seen; on the very first start it is the newest
// event on the bus, so old history never starts flows.
func (s *Server) cursor(ctx context.Context) int64 {
	if v, _, ok := s.store.CacheGet(ctx, cursorKey); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	var last int64
	if evs, err := s.core.Events(ctx, "", 0, 1); err == nil && len(evs) > 0 {
		last = evs[0].ID
	}
	_ = s.store.CachePut(ctx, cursorKey, strconv.FormatInt(last, 10))
	return last
}

func (s *Server) streamOnce(ctx context.Context, after int64) error {
	q := url.Values{"topics": {"*"}}
	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.CoreURL+"/api/v1/events/stream?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.core.Token())
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	if after > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(after, 10))
	}
	res, err := (&http.Client{}).Do(req) // long-lived: no timeout
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("Pocket Core answered %d %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	s.setStream(true, "")
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if data.Len() > 0 {
				var e core.Event
				if json.Unmarshal([]byte(data.String()), &e) == nil && e.ID > 0 {
					s.handleEvent(ctx, e)
					_ = s.store.CachePut(ctx, cursorKey, strconv.FormatInt(e.ID, 10))
				}
				data.Reset()
			}
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return sc.Err()
}

// eventOwner is the person an event belongs to.
func eventOwner(p map[string]any) string {
	for _, k := range []string{"user_email", "owner_email", "user"} {
		if v, ok := p[k].(string); ok && strings.Contains(v, "@") {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

// eventInput is what an event flow's placeholders see: the payload's fields,
// plus "event" (topic, app, subject, id, at).
func eventInput(e core.Event, payload map[string]any) map[string]any {
	in := map[string]any{}
	for k, v := range payload {
		in[k] = v
	}
	app, _, _ := strings.Cut(e.Topic, ".")
	in["event"] = map[string]any{"topic": e.Topic, "app": app, "source": e.Source, "subject": e.Subject, "id": e.ID, "at": e.CreatedAt, "name": catalog.TopicName(e.Topic)}
	return in
}

// handleEvent records a person's event and starts the flows it matches.
func (s *Server) handleEvent(ctx context.Context, e core.Event) {
	if e.Source == "flows" || strings.HasPrefix(e.Topic, "flows.") {
		return // never start flows from Flows' own events: no loops
	}
	var payload map[string]any
	if json.Unmarshal(e.Payload, &payload) != nil {
		return
	}
	email := eventOwner(payload)
	if email == "" {
		return
	}
	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return // not a person of this box (yet)
	}
	s.streamMu.Lock()
	s.lastEvent = time.Now()
	s.streamMu.Unlock()
	ev := &store.Event{CoreID: e.ID, Topic: e.Topic, Source: e.Source, Subject: e.Subject, Payload: payload}
	fresh, err := s.store.AddEvent(ctx, u.ID, ev)
	if err != nil || !fresh {
		return
	}
	if at, err := time.Parse(time.RFC3339Nano, e.CreatedAt); err == nil && time.Since(at) > maxEventAge {
		return
	}
	n := s.matchEvent(ctx, u.ID, e, payload)
	if n > 0 {
		_ = s.store.SetEventMatched(ctx, u.ID, ev.ID, n)
	}
}

// matchEvent queues a run of every enabled event flow of the person that fits.
func (s *Server) matchEvent(ctx context.Context, uid int64, e core.Event, payload map[string]any) int {
	flows, err := s.store.EventFlows(ctx, uid)
	if err != nil {
		return 0
	}
	input := eventInput(e, payload)
	v := flow.Vars{"trigger": input}
	n := 0
	for i := range flows {
		f := &flows[i]
		if !flow.TopicMatches(f.Trigger.Topic, e.Topic) || !flow.CheckAll(f.Trigger.Conditions, f.Trigger.Match, v) {
			continue
		}
		if r, err := s.enqueue(ctx, f, store.TriggerEvent, fmt.Sprintf("event:%d", e.ID), eventSummary(e.Topic, payload), input); err == nil && r != nil {
			n++
		}
	}
	return n
}

// eventSummary is one line about an event: "A price dropped: Lamp".
func eventSummary(topic string, p map[string]any) string {
	name := catalog.TopicName(topic)
	if name == "" {
		name = topic
	}
	for _, k := range []string{"title", "wish", "name", "label", "summary", "subject", "description", "habit", "metric"} {
		if v := flow.Text(p[k]); v != "" {
			return name + ": " + flow.Preview(v, 80)
		}
	}
	return name
}

// ---- schedules ----

// nextRun is when a flow runs next ("" = not on a schedule, or never again).
func nextRun(f *store.Flow, after time.Time) string {
	if f.TriggerKind != store.TriggerSchedule || !f.Enabled {
		return ""
	}
	t, ok := flow.Next(f.Trigger, after, time.Local)
	if !ok {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// runSchedules queues the schedule flows whose time has come. A flow that
// missed several times (the box was off) runs once, then moves on.
func (s *Server) runSchedules(ctx context.Context) {
	now := time.Now()
	due, err := s.store.DueFlows(ctx, now.UTC().Format(time.RFC3339))
	if err != nil {
		return
	}
	for i := range due {
		f := &due[i]
		at, _ := time.Parse(time.RFC3339, f.NextRunAt)
		summary := flow.DescribeSchedule(f.Trigger)
		input := map[string]any{"scheduled_for": at.In(time.Local).Format(time.RFC3339), "schedule": summary}
		if _, err := s.enqueue(ctx, f, store.TriggerSchedule, "schedule:"+f.NextRunAt, summary, input); err != nil {
			s.log.Warn("schedule", "flow", f.ID, "err", err)
		}
		_ = s.store.SetNextRun(ctx, f.UserID, f.ID, nextRun(f, now))
	}
}

// ---- incoming webhooks ----

const hooksPerMinute = 60

// hookInput turns a webhook request into the flow's input: a JSON object as
// it is, a JSON list as "items", a form as its fields, anything else as
// "text"; plus "query" and "method".
func hookInput(r *http.Request, body []byte) map[string]any {
	in := map[string]any{}
	ct := r.Header.Get("Content-Type")
	switch {
	case len(body) == 0:
	case strings.Contains(ct, "application/x-www-form-urlencoded"):
		if vals, err := url.ParseQuery(string(body)); err == nil {
			for k, v := range vals {
				in[k] = strings.Join(v, ", ")
			}
		}
	default:
		var obj map[string]any
		var list []any
		switch {
		case json.Unmarshal(body, &obj) == nil:
			in = obj
		case json.Unmarshal(body, &list) == nil:
			in["items"] = list
		default:
			in["text"] = string(body)
		}
	}
	q := map[string]any{}
	for k, v := range r.URL.Query() {
		q[k] = strings.Join(v, ", ")
		if _, taken := in[k]; !taken {
			in[k] = q[k]
		}
	}
	in["query"], in["method"] = q, r.Method
	return in
}

// handleHook starts a webhook flow: POST (or GET) /hooks/<secret>.
func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	ctx := r.Context()
	f, err := s.store.FlowByHook(ctx, tok)
	if err != nil || f.TriggerKind != store.TriggerWebhook {
		writeErr(w, http.StatusNotFound, "no flow at this address")
		return
	}
	if !f.Enabled {
		writeErr(w, http.StatusConflict, "this flow is switched off")
		return
	}
	s.hookMu.Lock()
	now := time.Now()
	recent := s.hookHit[tok][:0]
	for _, t := range s.hookHit[tok] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= hooksPerMinute {
		s.hookHit[tok] = recent
		s.hookMu.Unlock()
		w.Header().Set("Retry-After", "60")
		writeErr(w, http.StatusTooManyRequests, "too many calls; at most 60 a minute")
		return
	}
	s.hookHit[tok] = append(recent, now)
	s.hookMu.Unlock()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "the body is too large (1 MB at most)")
		return
	}
	input := hookInput(r, body)
	if !flow.CheckAll(f.Trigger.Conditions, f.Trigger.Match, flow.Vars{"trigger": input}) {
		writeJSON(w, http.StatusOK, map[string]any{"started": false, "reason": "conditions not met"})
		return
	}
	ref := "hook:" + store.RandomToken(9)
	if id := strings.TrimSpace(r.Header.Get("Idempotency-Key")); id != "" && len(id) <= 120 {
		ref = "hook:" + id
	}
	run, err := s.enqueue(ctx, f, store.TriggerWebhook, ref, "Webhook "+r.Method, input)
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if run == nil {
		writeJSON(w, http.StatusOK, map[string]any{"started": false, "reason": "already received (same Idempotency-Key)"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "run_id": run.ID})
}
