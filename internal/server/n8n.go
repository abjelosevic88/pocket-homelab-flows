package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// ---- n8n import ----
//
// An n8n workflow export (one workflow, a list, or n8n's {"data": [...]})
// becomes flows, switched off. The trigger (schedule, cron, webhook,
// manual), HTTP requests, waits and simple notifications carry over; every
// other node is named in the flow's description so nothing is lost silently.
// Importing the same workflow again changes nothing (external_id n8n:<id>).

type n8nNode struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Disabled   bool           `json:"disabled"`
	Parameters map[string]any `json:"parameters"`
}

type n8nWorkflow struct {
	ID          any                                     `json:"id"`
	Name        string                                  `json:"name"`
	Active      bool                                    `json:"active"`
	Nodes       []n8nNode                               `json:"nodes"`
	Connections map[string]map[string][][]n8nConnTarget `json:"connections"`
}

type n8nConnTarget struct {
	Node string `json:"node"`
}

type n8nResult struct {
	Name    string   `json:"name"`
	FlowID  int64    `json:"flow_id,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	Status  string   `json:"status"` // created | exists | failed
	Error   string   `json:"error,omitempty"`
}

func parseN8N(b []byte) ([]n8nWorkflow, error) {
	var one n8nWorkflow
	if json.Unmarshal(b, &one) == nil && len(one.Nodes) > 0 {
		return []n8nWorkflow{one}, nil
	}
	var many []n8nWorkflow
	if json.Unmarshal(b, &many) == nil && len(many) > 0 {
		return many, nil
	}
	var wrapped struct {
		Data []n8nWorkflow `json:"data"`
	}
	if json.Unmarshal(b, &wrapped) == nil && len(wrapped.Data) > 0 {
		return wrapped.Data, nil
	}
	return nil, fmt.Errorf("this does not look like an n8n workflow export")
}

var (
	n8nJSONDot = regexp.MustCompile(`\$json\.([A-Za-z0-9_.]+)`)
	n8nJSONIdx = regexp.MustCompile(`\$json\[\s*["']([^"']+)["']\s*\]`)
	n8nBody    = regexp.MustCompile(`\$json\.body\.`)
)

// n8nText turns an n8n parameter (maybe an "={{ $json.x }}" expression) into a Flows template.
func n8nText(v any) string {
	s, ok := v.(string)
	if !ok {
		if v == nil {
			return ""
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	if !strings.HasPrefix(s, "=") {
		return s
	}
	s = strings.TrimPrefix(s, "=")
	s = n8nBody.ReplaceAllString(s, "$$json.")
	s = n8nJSONIdx.ReplaceAllString(s, "$1")
	s = n8nJSONDot.ReplaceAllString(s, "$1")
	return s
}

func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func n8nTime(h, m any) string { return fmt.Sprintf("%02d:%02d", num(h), num(m)) }

// n8nTrigger maps a trigger node; ok=false when the node is not a trigger.
func n8nTrigger(n n8nNode) (kind string, t store.Trigger, note string, ok bool) {
	p := n.Parameters
	switch n.Type {
	case "n8n-nodes-base.manualTrigger", "n8n-nodes-base.start":
		return store.TriggerManual, t, "", true
	case "n8n-nodes-base.webhook":
		return store.TriggerWebhook, t, "", true
	case "n8n-nodes-base.scheduleTrigger":
		t = store.Trigger{Repeat: "daily", Time: "08:00"}
		rule, _ := p["rule"].(map[string]any)
		list, _ := rule["interval"].([]any)
		if len(list) == 0 {
			return store.TriggerSchedule, t, "", true
		}
		iv, _ := list[0].(map[string]any)
		switch iv["field"] {
		case "minutes":
			t = store.Trigger{Repeat: "minutes", Every: max(num(iv["minutesInterval"]), 5)}
		case "hours":
			t = store.Trigger{Repeat: "hourly", Every: max(num(iv["hoursInterval"]), 1), Time: n8nTime(0, iv["triggerAtMinute"])}
		case "days", nil:
			t = store.Trigger{Repeat: "daily", Time: n8nTime(iv["triggerAtHour"], iv["triggerAtMinute"])}
		case "weeks":
			days := ""
			if l, ok := iv["triggerAtDay"].([]any); ok {
				for _, d := range l {
					wd := num(d)
					if wd == 0 {
						wd = 7
					}
					days += strconv.Itoa(wd)
				}
			}
			t = store.Trigger{Repeat: "weekly", Days: firstNonEmpty(days, "1"), Time: n8nTime(iv["triggerAtHour"], iv["triggerAtMinute"])}
		case "months":
			t = store.Trigger{Repeat: "monthly", DayOfMonth: max(num(iv["triggerAtDayOfMonth"]), 1), Time: n8nTime(iv["triggerAtHour"], iv["triggerAtMinute"])}
		default:
			note = fmt.Sprintf("schedule %q set to every day at 08:00 — check it", iv["field"])
		}
		return store.TriggerSchedule, t, note, true
	case "n8n-nodes-base.cron":
		t = store.Trigger{Repeat: "daily", Time: "08:00"}
		tt, _ := p["triggerTimes"].(map[string]any)
		items, _ := tt["item"].([]any)
		if len(items) > 0 {
			it, _ := items[0].(map[string]any)
			switch it["mode"] {
			case "everyMinute", "everyX":
				t = store.Trigger{Repeat: "minutes", Every: max(num(it["value"]), 5)}
			case "everyHour":
				t = store.Trigger{Repeat: "hourly", Every: 1, Time: n8nTime(0, it["minute"])}
			case "everyWeek":
				wd := num(it["weekday"])
				if wd == 0 {
					wd = 7
				}
				t = store.Trigger{Repeat: "weekly", Days: strconv.Itoa(wd), Time: n8nTime(it["hour"], it["minute"])}
			case "everyMonth":
				t = store.Trigger{Repeat: "monthly", DayOfMonth: max(num(it["dayOfMonth"]), 1), Time: n8nTime(it["hour"], it["minute"])}
			default:
				t = store.Trigger{Repeat: "daily", Time: n8nTime(it["hour"], it["minute"])}
			}
		}
		return store.TriggerSchedule, t, "", true
	}
	if strings.HasSuffix(strings.ToLower(n.Type), "trigger") {
		return store.TriggerManual, t, "trigger “" + n.Name + "” (" + n.Type + ") has no Flows counterpart: the flow runs by hand", true
	}
	return "", t, "", false
}

var notifyNode = regexp.MustCompile(`(?i)(telegram|pushover|ntfy|gotify|discord|slack|mattermost|matrix|signal|apprise|pushbullet|emailsend|sendgrid|mailgun)`)

// n8nStep maps an action node; ok=false = not converted.
func n8nStep(n n8nNode, id string) (store.Step, bool) {
	p := n.Parameters
	switch n.Type {
	case "n8n-nodes-base.httpRequest":
		method := strings.ToUpper(firstNonEmpty(n8nText(p["method"]), firstNonEmpty(n8nText(p["requestMethod"]), "GET")))
		u := n8nText(p["url"])
		var headers []string
		if hp, ok := p["headerParameters"].(map[string]any); ok {
			if list, ok := hp["parameters"].([]any); ok {
				for _, h := range list {
					if m, ok := h.(map[string]any); ok {
						headers = append(headers, n8nText(m["name"])+": "+n8nText(m["value"]))
					}
				}
			}
		}
		body := n8nText(p["jsonBody"])
		if body == "" {
			if bp, ok := p["bodyParameters"].(map[string]any); ok {
				if list, ok := bp["parameters"].([]any); ok {
					obj := map[string]string{}
					for _, h := range list {
						if m, ok := h.(map[string]any); ok {
							obj[n8nText(m["name"])] = n8nText(m["value"])
						}
					}
					b, _ := json.Marshal(obj)
					body = string(b)
				}
			}
		}
		if body == "" {
			body = n8nText(p["body"])
		}
		return store.Step{ID: id, Kind: "http", Name: n.Name, Config: map[string]string{"method": method, "url": u, "headers": strings.Join(headers, "\n"), "body": body}}, u != ""
	case "n8n-nodes-base.wait":
		unit := n8nText(p["unit"])
		switch unit {
		case "minutes", "hours", "days":
		case "seconds":
			unit = "minutes"
		default:
			unit = "minutes"
		}
		return store.Step{ID: id, Kind: "wait", Name: n.Name, Config: map[string]string{"amount": firstNonEmpty(n8nText(p["amount"]), "1"), "unit": unit}}, true
	}
	if notifyNode.MatchString(n.Type) {
		body := ""
		for _, k := range []string{"text", "message", "content", "body", "html"} {
			if v := n8nText(p[k]); v != "" {
				body = v
				break
			}
		}
		title := firstNonEmpty(n8nText(p["title"]), firstNonEmpty(n8nText(p["subject"]), n.Name))
		return store.Step{ID: id, Kind: "notify", Name: n.Name, Config: map[string]string{"title": title, "body": body, "url": "", "priority": "default"}}, true
	}
	return store.Step{}, false
}

// convertN8N turns one workflow into a flow (not saved).
func convertN8N(w n8nWorkflow) (*store.Flow, []string) {
	byName := map[string]n8nNode{}
	for _, n := range w.Nodes {
		byName[n.Name] = n
	}
	f := &store.Flow{Name: firstNonEmpty(strings.TrimSpace(w.Name), "Imported from n8n"), TriggerKind: store.TriggerManual, Source: "n8n", Steps: []store.Step{}}
	var skipped []string
	start := ""
	for _, n := range w.Nodes {
		if kind, t, note, ok := n8nTrigger(n); ok && !n.Disabled {
			f.TriggerKind, f.Trigger, start = kind, t, n.Name
			if note != "" {
				skipped = append(skipped, note)
			}
			break
		}
	}
	// walk the main connections from the trigger, first branch first
	order := []string{}
	seen := map[string]bool{start: true}
	queue := []string{start}
	if start == "" {
		for _, n := range w.Nodes {
			queue = append(queue, n.Name)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur != start {
			order = append(order, cur)
		}
		for _, outs := range w.Connections[cur]["main"] {
			for _, t := range outs {
				if !seen[t.Node] {
					seen[t.Node] = true
					queue = append(queue, t.Node)
				}
			}
		}
	}
	for _, name := range order {
		n, ok := byName[name]
		if !ok || n.Disabled {
			continue
		}
		if _, _, _, isTrigger := n8nTrigger(n); isTrigger {
			continue
		}
		if n.Type == "n8n-nodes-base.noOp" || n.Type == "n8n-nodes-base.stickyNote" {
			continue
		}
		st, ok := n8nStep(n, fmt.Sprintf("s%d", len(f.Steps)+1))
		if !ok {
			skipped = append(skipped, "“"+n.Name+"” ("+strings.TrimPrefix(n.Type, "n8n-nodes-base.")+")")
			continue
		}
		if len(f.Steps) < maxSteps {
			f.Steps = append(f.Steps, st)
		}
	}
	desc := "Imported from n8n."
	if len(skipped) > 0 {
		desc += " Not converted, rebuild by hand: " + strings.Join(skipped, "; ") + "."
	}
	f.Description = flow.Preview(desc, 1900)
	if id := n8nText(w.ID); id != "" {
		f.ExternalID = "n8n:" + id
	} else {
		f.ExternalID = "n8n:" + f.Name
	}
	return f, skipped
}

func (s *Server) handleImportN8N(w http.ResponseWriter, r *http.Request) {
	b, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "could not read the file")
		return
	}
	wfs, err := parseN8N(b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	out := []n8nResult{}
	for _, wf := range wfs {
		f, skipped := convertN8N(wf)
		res := n8nResult{Name: f.Name, Skipped: skipped}
		if ex, err := s.store.FlowByExternal(ctx, uid, f.ExternalID); err == nil {
			res.Status, res.FlowID = "exists", ex.ID
			out = append(out, res)
			continue
		}
		f.Enabled = false
		if msg := s.checkFlow(f); msg != "" {
			res.Status, res.Error = "failed", msg
			out = append(out, res)
			continue
		}
		if err := s.store.CreateFlow(ctx, uid, f); err != nil {
			res.Status, res.Error = "failed", err.Error()
		} else {
			res.Status, res.FlowID = "created", f.ID
		}
		out = append(out, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}
