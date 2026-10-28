package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// appSetting is one sibling app as Settings → Connected apps shows it.
type appSetting struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Events    bool   `json:"events"`  // Flows can listen to it
	Actions   bool   `json:"actions"` // flows can act in it
	Mode      string `json:"mode"`    // in effect
	Via       string `json:"via"`     // "", core, direct
	Detail    string `json:"detail"`
	Listening bool   `json:"listening"`
	URL       string `json:"url"`
	Token     string `json:"token"` // masked
	PublicURL string `json:"public_url"`
	OpenURL   string `json:"open_url"`
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect and whether it works.
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart, "http_private": st.HTTPPrivate,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"notify_failures": st.NotifyFailures, "notify_recover": st.NotifyRecover, "keep_runs_days": st.KeepRunsDays,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL, "timezone": time.Now().Location().String(),
	}
	var states map[string]map[string]coreLinkState
	if u != nil {
		states = s.linkStates(ctx, u.Email)
	}
	apps := make([]appSetting, 0, len(pocketApps))
	for _, a := range pocketApps {
		var state map[string]coreLinkState
		if states != nil {
			state = states[a.slug]
		}
		mode, via, detail, listen := s.connStatus(ctx, u, st, a, state)
		c := connSettings(st, a.slug)
		as := appSetting{Slug: a.slug, Name: a.name, Events: a.events, Actions: a.actions, Mode: mode, Via: via, Detail: detail, Listening: listen,
			URL: c.URL, Token: mask(c.Token), PublicURL: c.PublicURL}
		if via != "" {
			as.OpenURL = firstNonEmpty(c.PublicURL, s.resolveSibling(ctx, a.slug))
		}
		apps = append(apps, as)
	}
	out["apps"] = apps
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

type connInput struct {
	Mode      *string `json:"mode"`
	URL       *string `json:"url"`
	Token     *string `json:"token"`
	PublicURL *string `json:"public_url"`
}

type settingsInput struct {
	store.Settings
	Connections map[string]connInput `json:"connections"`
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// and the local-network switch are only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in settingsInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["http_private"] && isAdmin {
		cur.HTTPPrivate = in.HTTPPrivate
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	if keys["keep_runs_days"] {
		if in.KeepRunsDays < 1 || in.KeepRunsDays > 365 {
			bad("keep the history 1 to 365 days")
			return
		}
		cur.KeepRunsDays = in.KeepRunsDays
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] {
		switch in.NotifyFormat {
		case "apprise", "ntfy", "json":
			cur.NotifyFormat = in.NotifyFormat
		default:
			bad("the notification format is apprise, ntfy or json")
			return
		}
	}
	boolIn("notify_failures", &cur.NotifyFailures, in.NotifyFailures)
	boolIn("notify_recover", &cur.NotifyRecover, in.NotifyRecover)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	modesChanged := false
	for slug, ci := range in.Connections {
		a, ok := appBySlug(slug)
		if !ok {
			bad("unknown app " + slug)
			return
		}
		c := cur.Connections[slug]
		if ci.Mode != nil {
			switch *ci.Mode {
			case modeCore, modeOff, "":
				c.Mode = *ci.Mode
			case modeDirect:
				if !a.actions {
					bad(a.name + " events only come through Pocket Core")
					return
				}
				c.Mode = modeDirect
			default:
				bad("a connection is core, direct or off")
				return
			}
			modesChanged = true
		}
		if ci.URL != nil {
			c.URL = strings.TrimRight(strings.TrimSpace(*ci.URL), "/")
		}
		if ci.Token != nil && !strings.Contains(*ci.Token, "••") {
			c.Token = strings.TrimSpace(*ci.Token)
			modesChanged = true
		}
		if ci.PublicURL != nil {
			c.PublicURL = strings.TrimRight(strings.TrimSpace(*ci.PublicURL), "/")
		}
		if c == (store.Conn{}) {
			delete(cur.Connections, slug)
		} else {
			cur.Connections[slug] = c
		}
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && (keys["title"] || keys["http_private"]) {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	if modesChanged {
		s.reportModesSoon()
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	u := userFrom(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		c := connSettings(st, a.slug)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(c.Token); tok != "" && (typed || s.connMode(c.Mode, c.Token) == modeDirect) {
			base := urlOr(c.URL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else if a.actions {
			conn, err = s.siblingFor(ctx, u, st, a, a.scopes[len(a.scopes)-1])
		} else {
			on, why := s.core.LinkOn(ctx, u.Email, a.slug, scopeEventsRead)
			if on {
				msg = "Flows hears your " + a.name + " events through Pocket Core"
			} else {
				err = fmt.Errorf("%s", firstNonEmpty(linkReason(a.name, why), why))
			}
		}
		if err == nil && conn != nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Flows", Body: "Notifications work. Your flows' messages and failure alerts will arrive here.", Priority: "default", Tags: []string{"zap"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core": s.coreStatus(), "stream": s.streamStatus(), "workers": s.cfg.Workers,
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "runs_since_start": s.ran.Load(), "failed_since_start": s.failed.Load(),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-flows-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's flows (webhook addresses are
// not exported: imported webhook flows get new ones).
type exportDoc struct {
	App      string       `json:"app"`
	Version  string       `json:"version"`
	Exported string       `json:"exported_at"`
	Flows    []store.Flow `json:"flows"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	doc := exportDoc{App: "pocket-flows", Version: Version, Exported: store.Now()}
	doc.Flows, _ = s.store.ListFlows(ctx, actorID(ctx))
	for i := range doc.Flows {
		if doc.Flows[i].ExternalID == "" {
			doc.Flows[i].ExternalID = fmt.Sprintf("pocket-flows:%d", doc.Flows[i].ID)
		}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-flows-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes the run history as CSV.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-flows-runs.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"run_id", "flow_id", "flow", "trigger", "summary", "status", "steps_done", "error", "created_at", "started_at", "finished_at"})
	var before int64
	for {
		runs, err := s.store.ListRuns(ctx, uid, store.RunFilter{Before: before, Limit: 200})
		if err != nil || len(runs) == 0 {
			break
		}
		for _, run := range runs {
			_ = cw.Write([]string{strconv.FormatInt(run.ID, 10), strconv.FormatInt(run.FlowID, 10), run.FlowName, run.TriggerKind, run.Summary, run.Status,
				strconv.Itoa(run.Step), run.Error, run.CreatedAt, run.StartedAt, run.FinishedAt})
		}
		before = runs[len(runs)-1].ID
	}
	cw.Flush()
}

// handleImportJSON adds the flows of an export to the caller's own. Flows
// keep their identity (external_id), so importing the same file twice adds
// nothing; imported flows start switched off.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-flows" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Flows export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{"flows": 0, "skipped": 0, "existing": 0}
	for _, f := range doc.Flows {
		ext := f.ExternalID
		if ext == "" {
			ext = fmt.Sprintf("pocket-flows:%d", f.ID)
		}
		if _, err := s.store.FlowByExternal(ctx, uid, ext); err == nil {
			counts["existing"]++
			continue
		}
		nf := &store.Flow{Name: f.Name, Description: f.Description, Enabled: false, TriggerKind: f.TriggerKind, Trigger: f.Trigger, Steps: f.Steps, Recipe: f.Recipe, Source: "import", ExternalID: ext}
		if nf.Steps == nil {
			nf.Steps = []store.Step{}
		}
		if msg := s.checkFlow(nf); msg != "" {
			counts["skipped"]++
			continue
		}
		if err := s.store.CreateFlow(ctx, uid, nf); err != nil {
			counts["skipped"]++
			continue
		}
		counts["flows"]++
	}
	writeJSON(w, http.StatusOK, counts)
}
