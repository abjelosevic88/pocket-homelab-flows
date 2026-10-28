package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

const (
	maxSteps      = 30
	maxConditions = 20
	maxFlows      = 500
)

var stepIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)

// flowView is a flow as the UI sees it: plus its webhook address and a
// sentence about its trigger.
type flowView struct {
	store.Flow
	HookURL     string `json:"hook_url,omitempty"`
	TriggerText string `json:"trigger_text"`
}

func (s *Server) hookURL(f *store.Flow) string {
	if f.TriggerKind != store.TriggerWebhook || f.HookToken == "" {
		return ""
	}
	base := s.cfg.BaseURL
	if base == "" {
		base = "http://localhost:" + fmt.Sprint(s.cfg.Port)
	}
	return base + "/hooks/" + f.HookToken
}

func triggerText(f *store.Flow) string {
	switch f.TriggerKind {
	case store.TriggerEvent:
		name := catalog.TopicName(f.Trigger.Topic)
		switch {
		case name != "":
		case strings.HasSuffix(f.Trigger.Topic, ".*"):
			app := strings.TrimSuffix(f.Trigger.Topic, ".*")
			if a, ok := catalog.AppBySlug(app); ok {
				name = "Anything in " + a.Name
			} else {
				name = "Anything in " + app
			}
		default:
			name = f.Trigger.Topic
		}
		if n := len(f.Trigger.Conditions); n > 0 {
			name += fmt.Sprintf(" (if %s)", describeAll(f.Trigger.Conditions, f.Trigger.Match))
		}
		return name
	case store.TriggerSchedule:
		return flow.DescribeSchedule(f.Trigger)
	case store.TriggerWebhook:
		return "When its web address is called"
	}
	return "When you run it"
}

func (s *Server) view(f *store.Flow, withHook bool) flowView {
	v := flowView{Flow: *f, TriggerText: triggerText(f)}
	if withHook {
		v.HookURL = s.hookURL(f)
	}
	return v
}

// checkFlow validates and normalises a flow before saving. "" = fine.
func (s *Server) checkFlow(f *store.Flow) string {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		f.Name = "New flow"
	}
	if len([]rune(f.Name)) > 120 {
		return "the name is too long (120 characters at most)"
	}
	if len(f.Description) > 2000 {
		return "the description is too long"
	}
	checkConds := func(cs []store.Condition, where string) string {
		if len(cs) > maxConditions {
			return fmt.Sprintf("%s: at most %d conditions", where, maxConditions)
		}
		for i := range cs {
			cs[i].Field = strings.TrimSpace(cs[i].Field)
			if cs[i].Field == "" {
				return where + ": a condition has no field"
			}
			if !flow.ValidOp(cs[i].Op) {
				return where + ": unknown comparison " + cs[i].Op
			}
		}
		return ""
	}
	t := &f.Trigger
	switch f.TriggerKind {
	case store.TriggerEvent:
		t.Topic = strings.TrimSpace(t.Topic)
		if t.Topic == "" {
			return "choose what happens: the event that starts the flow"
		}
		if !regexp.MustCompile(`^(\*|[a-z0-9_]+(\.[a-z0-9_*]+)*)(\s*,\s*[a-z0-9_]+(\.[a-z0-9_*]+)*)*$`).MatchString(t.Topic) {
			return "the event name is not valid"
		}
		*t = store.Trigger{Topic: t.Topic, Conditions: t.Conditions, Match: t.Match}
	case store.TriggerSchedule:
		if err := flow.CheckSchedule(t); err != nil {
			return "schedule: " + err.Error()
		}
		*t = store.Trigger{Repeat: t.Repeat, Every: t.Every, Time: t.Time, Days: t.Days, DayOfMonth: t.DayOfMonth, At: t.At}
	case store.TriggerWebhook:
		if f.HookToken == "" {
			f.HookToken = store.RandomToken(24)
		}
		*t = store.Trigger{Conditions: t.Conditions, Match: t.Match}
	case store.TriggerManual:
		*t = store.Trigger{}
	default:
		return "the trigger is an event, a schedule, a webhook or by hand"
	}
	if f.TriggerKind != store.TriggerWebhook {
		f.HookToken = ""
	}
	if msg := checkConds(t.Conditions, "trigger"); msg != "" {
		return msg
	}
	if t.Match != "any" {
		t.Match = ""
	}
	if len(f.Steps) > maxSteps {
		return fmt.Sprintf("a flow has at most %d steps", maxSteps)
	}
	ids := map[string]bool{}
	next := 1
	for i := range f.Steps {
		st := &f.Steps[i]
		k, ok := catalog.StepByKind(st.Kind)
		if !ok {
			return fmt.Sprintf("step %d: unknown kind %q", i+1, st.Kind)
		}
		if !stepIDRe.MatchString(st.ID) || ids[st.ID] {
			for ids[fmt.Sprintf("s%d", next)] {
				next++
			}
			st.ID = fmt.Sprintf("s%d", next)
		}
		ids[st.ID] = true
		st.Name = strings.TrimSpace(st.Name)
		if len(st.Name) > 120 {
			st.Name = st.Name[:120]
		}
		cfg := map[string]string{}
		for _, fl := range k.Fields {
			v := st.Config[fl.Key]
			if v == "" {
				v = fl.Default
			}
			if len(v) > 20000 {
				return fmt.Sprintf("step %d: %s is too long", i+1, fl.Label)
			}
			if fl.Type == "select" && v != "" {
				okv := false
				for _, o := range fl.Options {
					if o.Value == v {
						okv = true
					}
				}
				if !okv {
					return fmt.Sprintf("step %d: %s cannot be %q", i+1, fl.Label, v)
				}
			}
			cfg[fl.Key] = v
		}
		st.Config = cfg
		if msg := checkConds(st.OnlyIf, fmt.Sprintf("step %d", i+1)); msg != "" {
			return msg
		}
		if st.Kind == "filter" && len(st.OnlyIf) == 0 {
			return fmt.Sprintf("step %d: add at least one condition", i+1)
		}
		if st.Match != "any" {
			st.Match = ""
		}
		if st.OnError != "continue" {
			st.OnError = ""
		}
	}
	return ""
}

// ---- flows ----

func (s *Server) handleListFlows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListFlows(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "flows")
		return
	}
	out := make([]flowView, 0, len(list))
	link := linkApp(ctx) != ""
	for i := range list {
		out = append(out, s.view(&list[i], !link))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := s.store.GetFlow(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "flow")
		return
	}
	writeJSON(w, http.StatusOK, s.view(f, linkApp(ctx) == ""))
}

type flowInput struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Enabled     *bool         `json:"enabled"`
	TriggerKind string        `json:"trigger_kind"`
	Trigger     store.Trigger `json:"trigger"`
	Steps       []store.Step  `json:"steps"`
}

func (s *Server) handleCreateFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in flowInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if n := s.store.CountsFor(ctx, uid)["flows"]; n >= maxFlows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("you have %d flows, the most there can be", n))
		return
	}
	f := &store.Flow{Name: in.Name, Description: in.Description, Enabled: in.Enabled == nil || *in.Enabled, TriggerKind: firstNonEmpty(in.TriggerKind, store.TriggerManual), Trigger: in.Trigger, Steps: in.Steps}
	if f.Steps == nil {
		f.Steps = []store.Step{}
	}
	if msg := s.checkFlow(f); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	f.NextRunAt = nextRun(f, time.Now())
	if err := s.store.CreateFlow(ctx, uid, f); err != nil {
		s.fail(w, err, "create flow")
		return
	}
	s.publish(ctx, uid, "flows.flow_created", fmt.Sprintf("flow:%d", f.ID), map[string]any{"flow_id": f.ID, "flow": f.Name, "trigger": triggerText(f)})
	writeJSON(w, http.StatusCreated, s.view(f, true))
}

func (s *Server) handleUpdateFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFlow(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "flow")
		return
	}
	var in flowInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["name"] {
		f.Name = in.Name
	}
	if keys["description"] {
		f.Description = in.Description
	}
	if keys["enabled"] && in.Enabled != nil {
		f.Enabled = *in.Enabled
	}
	if keys["trigger_kind"] {
		f.TriggerKind = in.TriggerKind
	}
	if keys["trigger"] {
		f.Trigger = in.Trigger
	}
	if keys["steps"] {
		f.Steps = in.Steps
		if f.Steps == nil {
			f.Steps = []store.Step{}
		}
	}
	if msg := s.checkFlow(f); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	f.NextRunAt = nextRun(f, time.Now())
	if err := s.store.UpdateFlow(ctx, uid, f); err != nil {
		s.fail(w, err, "save flow")
		return
	}
	writeJSON(w, http.StatusOK, s.view(f, true))
}

func (s *Server) handleSetEnabled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	f, err := s.store.GetFlow(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "flow")
		return
	}
	f.Enabled = in.Enabled
	f.NextRunAt = nextRun(f, time.Now())
	if err := s.store.SetFlowEnabled(ctx, uid, f.ID, f.Enabled, f.NextRunAt); err != nil {
		s.fail(w, err, "flow")
		return
	}
	writeJSON(w, http.StatusOK, s.view(f, true))
}

func (s *Server) handleDeleteFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteFlow(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete flow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDuplicateFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFlow(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "flow")
		return
	}
	c := &store.Flow{Name: flow.Preview(f.Name+" (copy)", 120), Description: f.Description, Enabled: false, TriggerKind: f.TriggerKind, Trigger: f.Trigger, Steps: f.Steps, Recipe: f.Recipe}
	if msg := s.checkFlow(c); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateFlow(ctx, uid, c); err != nil {
		s.fail(w, err, "duplicate flow")
		return
	}
	writeJSON(w, http.StatusCreated, s.view(c, true))
}

// handleRotateHook gives a webhook flow a new secret address.
func (s *Server) handleRotateHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFlow(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "flow")
		return
	}
	if f.TriggerKind != store.TriggerWebhook {
		writeErr(w, http.StatusBadRequest, "this flow does not start from a web address")
		return
	}
	f.HookToken = store.RandomToken(24)
	if err := s.store.UpdateFlow(ctx, uid, f); err != nil {
		s.fail(w, err, "flow")
		return
	}
	writeJSON(w, http.StatusOK, s.view(f, true))
}

// handleRunFlow runs a flow now: with the input given, or with one of the
// person's recorded events ({"event_id": n}) to try an event flow.
func (s *Server) handleRunFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFlow(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "flow")
		return
	}
	var in struct {
		Input   map[string]any `json:"input"`
		EventID int64          `json:"event_id"`
	}
	_ = decode(r, &in)
	if app := linkApp(ctx); app != "" && f.TriggerKind != store.TriggerManual {
		writeErr(w, http.StatusForbidden, "only flows that run by hand can be started by another app")
		return
	}
	input, summary := in.Input, "Run by hand"
	if app := linkApp(ctx); app != "" {
		summary = "Started by Pocket " + strings.ToUpper(app[:1]) + app[1:]
	}
	if in.EventID > 0 {
		e, err := s.store.GetEvent(ctx, uid, in.EventID)
		if err != nil {
			s.fail(w, err, "event")
			return
		}
		input = eventInputStored(e)
		summary = "Tried with: " + eventSummary(e.Topic, e.Payload)
	}
	if input == nil {
		input = map[string]any{}
	}
	if f.TriggerKind == store.TriggerSchedule {
		if _, ok := input["scheduled_for"]; !ok {
			input["scheduled_for"] = time.Now().Format(time.RFC3339)
		}
	}
	run, err := s.enqueue(ctx, f, store.TriggerManual, "manual:"+store.RandomToken(9), summary, input)
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func eventInputStored(e *store.Event) map[string]any {
	in := map[string]any{}
	for k, v := range e.Payload {
		in[k] = v
	}
	app, _, _ := strings.Cut(e.Topic, ".")
	in["event"] = map[string]any{"topic": e.Topic, "app": app, "source": e.Source, "subject": e.Subject, "id": e.CoreID, "at": e.ReceivedAt, "name": catalog.TopicName(e.Topic)}
	return in
}

// handleFlowFields lists the placeholders a flow's steps can use: the
// trigger's fields (from the newest matching event, or the webhook's last
// call) and what each earlier step gives.
func (s *Server) handleFlowFields(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	topic := r.URL.Query().Get("topic")
	kind := r.URL.Query().Get("kind")
	var sample map[string]any
	if id := idParam(r, "id"); id > 0 && kind == store.TriggerWebhook {
		if runs, err := s.store.ListRuns(ctx, uid, store.RunFilter{FlowID: id, Limit: 1}); err == nil && len(runs) > 0 {
			if full, err := s.store.GetRun(ctx, uid, runs[0].ID); err == nil {
				sample = full.Input
			}
		}
	}
	if kind == store.TriggerEvent && topic != "" {
		if evs, err := s.store.ListEvents(ctx, uid, strings.Split(topic, ",")[0], 0, 1); err == nil && len(evs) > 0 {
			sample = eventInputStored(&evs[0])
		}
	}
	fields := []string{}
	if sample != nil {
		fields = flow.Fields(sample, 3)
	}
	if kind == store.TriggerSchedule {
		fields = []string{"scheduled_for", "schedule"}
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields, "sample": sample,
		"always": []string{"today", "tomorrow", "time", "now", "weekday", "me.name", "me.email", "flow.name", "flow.url"}})
}

// ---- runs ----

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := store.RunFilter{FlowID: int64(queryInt(r, "flow")), Status: r.URL.Query().Get("status"), Before: int64(queryInt(r, "before")), Limit: queryInt(r, "limit")}
	list, err := s.store.ListRuns(ctx, actorID(ctx), f)
	if err != nil {
		s.fail(w, err, "runs")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	run, err := s.store.GetRun(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "run")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	if err := s.store.RetryRun(ctx, uid, idParam(r, "id")); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "only failed or cancelled runs can be tried again")
			return
		}
		s.fail(w, err, "retry")
		return
	}
	s.nudge()
	run, _ := s.store.GetRun(ctx, uid, idParam(r, "id"))
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	if err := s.store.CancelRun(ctx, uid, idParam(r, "id")); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "only waiting or queued runs can be cancelled")
			return
		}
		s.fail(w, err, "cancel")
		return
	}
	run, _ := s.store.GetRun(ctx, uid, idParam(r, "id"))
	writeJSON(w, http.StatusOK, run)
}

// ---- events ----

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListEvents(ctx, actorID(ctx), r.URL.Query().Get("topic"), int64(queryInt(r, "before")), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "events")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleEventTopics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.SeenTopics(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "topics")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---- overview, catalog, pickers, recipes ----

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	now := time.Now()
	ds := dayStart(now)
	utc := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
	st := s.store.Stats(ctx, uid, utc(ds), utc(ds.AddDate(0, 0, -6)))
	_, off := now.Zone()
	days, _ := s.store.RunsByDay(ctx, uid, utc(ds.AddDate(0, 0, -13)), off/60)
	recent, _ := s.store.ListRuns(ctx, uid, store.RunFilter{Limit: 8})
	failing := []flowView{}
	upcoming := []flowView{}
	if list, err := s.store.ListFlows(ctx, uid); err == nil {
		for i := range list {
			f := &list[i]
			if f.Enabled && f.LastStatus == store.RunFailed {
				failing = append(failing, s.view(f, false))
			}
			if f.Enabled && f.NextRunAt != "" {
				upcoming = append(upcoming, s.view(f, false))
			}
		}
	}
	for i := 1; i < len(upcoming); i++ {
		for j := i; j > 0 && upcoming[j].NextRunAt < upcoming[j-1].NextRunAt; j-- {
			upcoming[j], upcoming[j-1] = upcoming[j-1], upcoming[j]
		}
	}
	if len(upcoming) > 5 {
		upcoming = upcoming[:5]
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": st, "days": days, "recent": recent, "failing": failing, "upcoming": upcoming, "stream": s.streamStatus()})
}

// catalogApp is an app in the catalog with the person's link state.
type catalogApp struct {
	catalog.App
	Listening bool   `json:"listening"` // Flows hears its events for you
	Acting    string `json:"acting"`    // "", core, direct: how steps reach it
	Detail    string `json:"detail"`
	Actions   bool   `json:"actions"`
	OpenURL   string `json:"open_url"`
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _ := s.store.GetSettings(ctx, u.ID)
	states := s.linkStates(ctx, u.Email)
	apps := make([]catalogApp, 0, len(pocketApps))
	for _, a := range pocketApps {
		ca := catalogApp{App: catalog.App{Slug: a.slug, Name: a.name, Events: []catalog.Topic{}}, Actions: a.actions}
		if app, ok := catalog.AppBySlug(a.slug); ok {
			ca.App = app
		}
		var state map[string]coreLinkState
		if states != nil {
			state = states[a.slug]
		}
		_, via, detail, listen := s.connStatus(ctx, u, st, a, state)
		ca.Listening, ca.Acting, ca.Detail = listen, via, detail
		if via != "" {
			ca.OpenURL = firstNonEmpty(connSettings(st, a.slug).PublicURL, s.resolveSibling(ctx, a.slug))
		}
		apps = append(apps, ca)
	}
	ops := make([]catalog.Option, 0, len(flow.Ops))
	for _, o := range flow.Ops {
		ops = append(ops, catalog.Option{Value: o.Key, Label: o.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "steps": catalog.Steps, "ops": ops,
		"core": s.core.Configured() && s.coreStatus()["registered"] == true, "stream": s.streamStatus()})
}

func (s *Server) handlePicker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _ := s.store.GetSettings(ctx, u.ID)
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	opts, err := s.pickerOptions(c, u, st, r.URL.Query().Get("name"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "options": []catalog.Option{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "options": opts})
}

func (s *Server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, recipes)
}

// handleUseRecipe creates a flow from a recipe, switched off.
func (s *Server) handleUseRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	rc, ok := recipeByKey(chiParam(r, "key"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	f := rc.Flow
	f.Steps = append([]store.Step(nil), rc.Flow.Steps...)
	for i := range f.Steps {
		cfg := map[string]string{}
		for k, v := range f.Steps[i].Config {
			cfg[k] = v
		}
		f.Steps[i].Config = cfg
	}
	f.Enabled, f.Recipe, f.Description = false, rc.Key, rc.Description
	if msg := s.checkFlow(&f); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateFlow(ctx, uid, &f); err != nil {
		s.fail(w, err, "create flow")
		return
	}
	writeJSON(w, http.StatusCreated, s.view(&f, true))
}
