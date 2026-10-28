package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// doStep does one step with its placeholders filled in. It returns the
// step's output (what later steps can use) and one line for the run log.
func (s *Server) doStep(ctx context.Context, r *store.Run, f *store.Flow, step store.Step, cfg map[string]string, v flow.Vars, u *store.User, st store.Settings) (map[string]any, string, error) {
	switch step.Kind {
	case "notify":
		return s.stepNotify(ctx, cfg, st)
	case "http":
		return s.stepHTTP(ctx, cfg, step, v, u, st)
	case "ai":
		return s.stepAI(ctx, r.UserID, cfg, st)
	}
	k, ok := catalog.StepByKind(step.Kind)
	if !ok || k.App == "" {
		return nil, "", permanent("unknown step %q", step.Kind)
	}
	a, ok := appBySlug(k.App)
	if !ok {
		return nil, "", permanent("unknown app %q", k.App)
	}
	sib, err := s.siblingFor(ctx, u, st, a, k.Scopes[0])
	if err != nil {
		return nil, "", err
	}
	idem := map[string]string{"Idempotency-Key": fmt.Sprintf("flows-run-%d-%s-%d", r.ID, step.ID, r.Step)}
	return s.stepApp(ctx, sib, step.Kind, cfg, r, step, idem)
}

func (s *Server) stepNotify(ctx context.Context, cfg map[string]string, st store.Settings) (map[string]any, string, error) {
	if st.NotifyURL == "" {
		return nil, "", permanent("set where notifications go in Settings → Notifications")
	}
	title := strings.TrimSpace(cfg["title"])
	if title == "" {
		title = "Pocket Flows"
	}
	m := notify.Message{Title: title, Body: cfg["body"], Link: strings.TrimSpace(cfg["url"]), Priority: firstNonEmpty(cfg["priority"], "default")}
	if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, m); err != nil {
		return nil, "", &stepErr{msg: err.Error(), retry: true}
	}
	return map[string]any{"title": m.Title, "body": m.Body}, "Sent “" + flow.Preview(title, 80) + "”", nil
}

// ---- Call a web address ----

// errPrivate is returned when a member's request would reach the local network.
var errPrivate = errors.New("this address is on the local network; an admin can allow that in Settings → General")

// privateIP reports loopback, private, link-local and similar addresses.
func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast() || (ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) // 100.64/10 (CGNAT, tailnets)
}

// newStepClient is an HTTP client for web steps. Unless allowPrivate, its
// dialer refuses private addresses after DNS (so a public name that resolves
// to 192.168.x.x is refused too). The two kinds never share a connection
// pool, so a connection an admin opened cannot be reused by a member.
func newStepClient(allowPrivate bool) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && privateIP(ip) {
				return errPrivate
			}
			return nil
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = d.DialContext
	if !allowPrivate {
		tr.Proxy = nil // a proxy would dial for us and hide the address
	}
	return &http.Client{Transport: tr, Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

func (s *Server) stepHTTP(ctx context.Context, cfg map[string]string, step store.Step, v flow.Vars, u *store.User, st store.Settings) (map[string]any, string, error) {
	method := strings.ToUpper(firstNonEmpty(step.Config["method"], "POST"))
	raw := strings.TrimSpace(cfg["url"])
	pu, err := url.Parse(raw)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return nil, "", permanent("the address must start with http:// or https://")
	}
	client := s.http
	if (u != nil && u.Role == "admin") || st.HTTPPrivate {
		client = s.httpLocal
	}
	var body io.Reader
	ctype := ""
	b := strings.TrimSpace(cfg["body"])
	if b == "" && method != http.MethodGet && method != http.MethodDelete {
		j, _ := json.Marshal(v["trigger"])
		b = string(j)
	}
	if b != "" && method != http.MethodGet {
		body = strings.NewReader(b)
		ctype = "text/plain; charset=utf-8"
		if json.Valid([]byte(b)) {
			ctype = "application/json"
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, pu.String(), body)
	if err != nil {
		return nil, "", permanent("%v", err)
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for _, line := range strings.Split(cfg["headers"], "\n") {
		k, val, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) != "" {
			req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(val))
		}
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errPrivate) {
			return nil, "", permanent("%s", errPrivate.Error())
		}
		return nil, "", &stepErr{msg: strings.TrimPrefix(err.Error(), method+" "), retry: true}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	out := map[string]any{"status": res.StatusCode}
	text := string(data)
	if len(text) > 64<<10 {
		text = text[:64<<10]
	}
	out["body"] = text
	var parsed any
	if json.Unmarshal(data, &parsed) == nil {
		out["json"] = parsed
	}
	if res.StatusCode >= 300 {
		msg := fmt.Sprintf("the address answered %d %s", res.StatusCode, flow.Preview(text, 160))
		return nil, "", &stepErr{msg: strings.TrimSpace(msg), retry: res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests}
	}
	return out, fmt.Sprintf("%s %s → %d", method, pu.Host, res.StatusCode), nil
}

// ---- Ask AI ----

const flowAIVoice = `You are a step inside a personal automation. Do exactly what the instruction asks with the data given, briefly and plainly. Never invent facts that are not in the data. No preamble, no sign-off.`

func (s *Server) stepAI(ctx context.Context, uid int64, cfg map[string]string, st store.Settings) (map[string]any, string, error) {
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		return nil, "", permanent("no AI connected: %s (Settings → AI)", firstNonEmpty(why, "add Pocket AI or an OpenAI key"))
	}
	prompt := strings.TrimSpace(cfg["prompt"])
	if prompt == "" {
		return nil, "", permanent("the question is empty")
	}
	jsonMode := cfg["json"] == "true"
	if jsonMode {
		prompt += "\n\nAnswer with one JSON object only."
	}
	text, err := cl.Complete(ctx, flowAIVoice, prompt, jsonMode)
	if err != nil {
		return nil, "", &stepErr{msg: err.Error(), retry: true}
	}
	text = strings.TrimSpace(text)
	out := map[string]any{"text": text, "provider": used}
	if jsonMode {
		var obj map[string]any
		clean := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```")
		if json.Unmarshal([]byte(strings.TrimSpace(clean)), &obj) == nil {
			out["json"] = obj
			for k, val := range obj {
				if _, taken := out[k]; !taken {
					out[k] = val
				}
			}
		}
	}
	return out, flow.Preview(text, 120), nil
}

// ---- steps in sibling apps ----

func intConfig(v string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n, err == nil && n != 0
}

// unwrap returns the object an app answered with ({"task": {...}} → the task).
func unwrap(resp map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		if m, ok := resp[k].(map[string]any); ok {
			return m
		}
	}
	return resp
}

func pick(m map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (s *Server) stepApp(ctx context.Context, sib *siblingConn, kind string, cfg map[string]string, r *store.Run, step store.Step, idem map[string]string) (map[string]any, string, error) {
	c := sib.client
	var resp map[string]any
	via := ""
	if sib.via == modeDirect {
		via = " (direct)"
	}
	switch kind {
	case "tasks.add":
		text := strings.TrimSpace(cfg["text"])
		if text == "" {
			return nil, "", permanent("the task is empty")
		}
		body := map[string]any{"text": text, "commit": true}
		if id, ok := intConfig(step.Config["list_id"]); ok {
			body["list_id"] = id
		}
		if err := c.Do(ctx, http.MethodPost, "/api/v1/tasks/quick", body, idem, &resp); err != nil {
			return nil, "", err
		}
		t := unwrap(resp, "task")
		return pick(t, "id", "title", "due", "list_id"), "Task added: " + flow.Preview(flow.Text(t["title"]), 80) + via, nil
	case "notes.capture":
		text := strings.TrimSpace(cfg["text"])
		if text == "" {
			return nil, "", permanent("the text is empty")
		}
		if err := c.Do(ctx, http.MethodPost, "/api/v1/capture", map[string]any{"text": text}, idem, &resp); err != nil {
			return nil, "", err
		}
		return pick(resp, "id", "title"), "Added to today's note" + via, nil
	case "notes.create":
		title := strings.TrimSpace(cfg["title"])
		if title == "" {
			return nil, "", permanent("the note needs a title")
		}
		body := map[string]any{"title": title, "content": cfg["content"]}
		if f := strings.TrimSpace(step.Config["category"]); f != "" {
			body["category"] = f
		}
		if err := c.Do(ctx, http.MethodPost, "/api/v1/notes", body, idem, &resp); err != nil {
			return nil, "", err
		}
		return pick(unwrap(resp, "note"), "id", "title"), "Note created: " + flow.Preview(title, 80) + via, nil
	case "calendar.add":
		text := strings.TrimSpace(cfg["text"])
		if text == "" {
			return nil, "", permanent("the event is empty")
		}
		body := map[string]any{"text": text}
		if id, ok := intConfig(step.Config["calendar_id"]); ok {
			body["calendar_id"] = id
		}
		if err := c.Do(ctx, http.MethodPost, "/api/v1/events/quick", body, idem, &resp); err != nil {
			return nil, "", err
		}
		e := unwrap(resp, "event")
		return pick(e, "id", "title", "start", "end"), "Event added: " + flow.Preview(firstNonEmpty(flow.Text(e["title"]), text), 80) + via, nil
	case "money.add":
		acct, ok := intConfig(step.Config["account_id"])
		if !ok {
			return nil, "", permanent("choose an account")
		}
		amt, ok := flow.Number(cfg["amount"])
		if !ok || amt == 0 {
			return nil, "", permanent("the amount %q is not a number", cfg["amount"])
		}
		desc := strings.TrimSpace(cfg["description"])
		if desc == "" {
			return nil, "", permanent("the transaction needs a description")
		}
		date := strings.TrimSpace(cfg["date"])
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		if len(date) > 10 {
			date = date[:10]
		}
		kind := firstNonEmpty(step.Config["kind"], "expense")
		body := map[string]any{"account_id": acct, "amount": int64(math.Round(math.Abs(amt) * 100)), "kind": kind, "date": date,
			"description": desc, "external_id": fmt.Sprintf("pocket-flows:%d:%s", r.ID, step.ID)}
		if p := strings.TrimSpace(cfg["payee"]); p != "" {
			body["payee"] = p
		}
		if id, ok := intConfig(step.Config["category_id"]); ok {
			body["category_id"] = id
		}
		if err := c.Do(ctx, http.MethodPost, "/api/v1/transactions", body, idem, &resp); err != nil {
			return nil, "", err
		}
		return pick(unwrap(resp, "transaction"), "id", "amount", "date"), fmt.Sprintf("%s of %.2f recorded%s", strings.ToUpper(kind[:1])+kind[1:], math.Abs(amt), via), nil
	case "habits.mark":
		id, ok := intConfig(step.Config["habit_id"])
		if !ok {
			return nil, "", permanent("choose a habit")
		}
		date := strings.TrimSpace(cfg["date"])
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		status := 1
		if step.Config["status"] == "-1" {
			status = -1
		}
		if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/habits/%d/entries/%s", id, url.PathEscape(date)), map[string]any{"status": status}, idem, nil); err != nil {
			return nil, "", err
		}
		return map[string]any{"habit_id": id, "date": date, "status": status}, "Habit marked for " + date + via, nil
	case "inventory.adjust":
		id, ok := intConfig(step.Config["item_id"])
		if !ok {
			return nil, "", permanent("choose an item")
		}
		delta, ok := flow.Number(cfg["delta"])
		if !ok || delta == 0 {
			return nil, "", permanent("change by must be a number other than 0")
		}
		if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/adjust", id), map[string]any{"delta": delta, "note": firstNonEmpty(cfg["note"], "Pocket Flows")}, idem, &resp); err != nil {
			return nil, "", err
		}
		it := unwrap(resp, "item")
		return pick(it, "id", "name", "quantity"), fmt.Sprintf("%s changed by %s%s", firstNonEmpty(flow.Text(it["name"]), "Item"), flow.Text(delta), via), nil
	case "projects.issue":
		team := strings.TrimSpace(step.Config["team"])
		title := strings.TrimSpace(cfg["title"])
		if team == "" || title == "" {
			return nil, "", permanent("choose a team and give the issue a title")
		}
		body := map[string]any{"team": team, "title": title, "description": cfg["description"]}
		if p, err := strconv.Atoi(step.Config["priority"]); err == nil && p > 0 {
			body["priority"] = p
		}
		if err := c.Do(ctx, http.MethodPost, "/api/v1/issues", body, idem, &resp); err != nil {
			return nil, "", err
		}
		is := unwrap(resp, "issue")
		return pick(is, "id", "identifier", "title"), "Issue " + firstNonEmpty(flow.Text(is["identifier"]), "") + " created" + via, nil
	}
	return nil, "", permanent("unknown step %q", kind)
}

// ---- pickers ----

// pickerOptions reads a list from a sibling app for a step's select.
func (s *Server) pickerOptions(ctx context.Context, u *store.User, st store.Settings, name string) ([]catalog.Option, error) {
	p, ok := catalog.Pickers[name]
	if !ok {
		return nil, permanent("unknown list")
	}
	a, ok := appBySlug(p.App)
	if !ok {
		return nil, permanent("unknown app")
	}
	sib, err := s.siblingFor(ctx, u, st, a, p.Scope)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := sib.client.Do(ctx, http.MethodGet, p.Path, nil, nil, &raw); err != nil {
		return nil, err
	}
	list, _ := raw.([]any)
	if m, ok := raw.(map[string]any); ok && list == nil {
		for _, v := range m {
			if l, ok := v.([]any); ok {
				list = l
				break
			}
		}
	}
	out := []catalog.Option{}
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		val := flow.Text(m[p.Value])
		if val == "" {
			continue
		}
		label := ""
		for _, k := range []string{"name", "title", "path", "key"} {
			if t := flow.Text(m[k]); t != "" {
				label = t
				break
			}
		}
		if k := flow.Text(m["key"]); k != "" && p.Value == "key" && label != k {
			label = k + " — " + label
		}
		if archived, _ := m["archived"].(bool); archived {
			continue
		}
		out = append(out, catalog.Option{Value: val, Label: firstNonEmpty(label, val)})
	}
	return out, nil
}
