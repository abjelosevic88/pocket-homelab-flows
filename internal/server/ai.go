package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

// handleDraft turns a sentence ("when a price drops on my wishlist, send me a
// push") into a flow draft with the catalog's events and steps. Nothing is
// saved: the editor opens with the draft.
func (s *Server) handleDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, _, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": firstNonEmpty(why, "no AI connected (Settings → AI)")})
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeErr(w, http.StatusBadRequest, "describe the flow")
		return
	}
	var b strings.Builder
	b.WriteString("Events that can start a flow (topic: meaning):\n")
	for _, a := range catalog.Apps {
		for _, t := range a.Events {
			b.WriteString(t.Topic + ": " + t.Name + "\n")
		}
	}
	b.WriteString("\nStep kinds (kind: what it does; config keys):\n")
	for _, k := range catalog.Steps {
		keys := []string{}
		for _, f := range k.Fields {
			keys = append(keys, f.Key)
		}
		b.WriteString(k.Kind + ": " + k.Name + "; config " + strings.Join(keys, ", ") + "\n")
	}
	prompt := "Build a Pocket Flows automation from this request: " + strings.TrimSpace(in.Text) + "\n\n" + b.String() + `
Answer with ONE JSON object: {"name": short name, "description": one sentence, "trigger_kind": "event"|"schedule"|"webhook"|"manual",
"trigger": for event {"topic": a topic above, "conditions": [{"field","op","value"}]}; for schedule {"repeat": "minutes"|"hourly"|"daily"|"weekly"|"monthly", "every": n, "time": "HH:MM", "days": ISO weekdays like "12345", "day_of_month": n},
"steps": [{"id": "s1", "kind": a kind above, "config": {key: value strings}, "only_if": [conditions]}]}.
Comparisons (op): equals, not_equals, contains, not_contains, starts_with, ends_with, gt, gte, lt, lte, exists, missing, matches.
In config values use placeholders {{field}} for fields of the event or webhook body (e.g. {{title}}), {{steps.s1.text}} for an earlier step's answer, {{today}}, {{time}}, {{me.name}}.
Leave picker ids (list_id, account_id, calendar_id, habit_id, item_id, team, category, category_id) empty: the person picks them.
Use only the kinds and topics listed. Keep it to the fewest steps that do what was asked.`
	text, err := cl.Complete(ctx, "You design small personal automations. Answer with JSON only.", prompt, true)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(text), "```json"), "```"), "```"))
	var draft flowInput
	if err := json.Unmarshal([]byte(text), &draft); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "the AI's answer was not a flow; try saying it differently"})
		return
	}
	f := &store.Flow{Name: draft.Name, Description: draft.Description, Enabled: false, TriggerKind: firstNonEmpty(draft.TriggerKind, store.TriggerManual), Trigger: draft.Trigger, Steps: draft.Steps}
	if f.Steps == nil {
		f.Steps = []store.Step{}
	}
	if msg := s.checkFlow(f); msg != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "the draft did not work out (" + msg + "); try saying it differently"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "flow": s.view(f, false)})
}
