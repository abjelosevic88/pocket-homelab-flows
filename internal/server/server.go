// Package server wires the HTTP API, auth, the flow engine and the embedded SPA.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/config"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	ran      atomic.Int64 // runs finished since start
	failed   atomic.Int64

	wake      chan struct{} // a run was queued
	http      *http.Client  // "Call a web address" steps: public addresses only
	httpLocal *http.Client  // … and for admins (or when allowed) the local network too
	hookMu    sync.Mutex
	hookHit   map[string][]time.Time // webhook calls per address, the last minute

	streamMu  sync.Mutex
	streamOK  bool
	streamErr string
	lastEvent time.Time

	oidcMu sync.Mutex
	oidc   *oidcProvider // for the issuer in use; rebuilt when it changes

	core     *core.Client
	coreMu   sync.Mutex
	coreOK   bool
	coreErr  string
	resolved map[string]resolvedURL
	modesMu  sync.Mutex // one connection-mode report at a time
}

type resolvedURL struct {
	url string
	at  time.Time
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), oidc: &oidcProvider{},
		core:     core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: cfg.UserAgent}),
		resolved: map[string]resolvedURL{}, wake: make(chan struct{}, 1), hookHit: map[string][]time.Time{},
		http: newStepClient(false), httpLocal: newStepClient(true)}
}

// Bootstrap seeds the first admin (if configured), the owner's integration
// settings from the environment and demo data, then starts the background work.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if owner := s.store.OwnerID(ctx); owner > 0 {
		if err := s.ownerReady(ctx, owner); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	if n, err := s.store.RequeueRunning(ctx); err == nil && n > 0 {
		s.log.Info("runs a restart interrupted were queued again", "count", n)
	}
	for i := 0; i < s.cfg.Workers; i++ {
		go s.worker(ctx)
	}
	go s.janitor(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
		go s.listen(ctx)
	}
	return nil
}

// ownerReady runs once the box owner (the first admin) exists: the
// integration settings from the environment are the owner's — seeded into the
// owner's personal settings only, never into anyone else's — and demo data
// lands in the owner's account.
func (s *Server) ownerReady(ctx context.Context, owner int64) error {
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return err
	}
	changed := false
	once := func(name string, empty bool, v string, apply func()) {
		key := fmt.Sprintf("seeded:%d:%s", owner, name)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			return
		}
		if empty && v != "" {
			apply()
			changed = true
			empty = false
		}
		if !empty {
			_ = s.store.CachePut(ctx, key, "1")
		}
	}
	set := func(name string, dst *string, v string) {
		once(name, *dst == "", v, func() { *dst = v })
	}
	set("NotifyURL", &st.NotifyURL, s.cfg.NotifyURL)
	set("OpenAIKey", &st.OpenAIKey, s.cfg.OpenAIKey)
	set("OpenAIModel", &st.OpenAIModel, s.cfg.OpenAIModel)
	set("OpenAIBaseURL", &st.OpenAIBaseURL, s.cfg.OpenAIBaseURL)
	if s.cfg.NotifyFormat != "" && st.NotifyURL == s.cfg.NotifyURL && st.NotifyFormat != s.cfg.NotifyFormat {
		st.NotifyFormat = s.cfg.NotifyFormat
		changed = true
	}
	if changed {
		if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
			return err
		}
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

// janitor runs the background chores: schedules every 20 seconds, the
// history clean-up once an hour.
func (s *Server) janitor(ctx context.Context) {
	tick := time.NewTicker(20 * time.Second)
	hour := time.NewTicker(time.Hour)
	defer tick.Stop()
	defer hour.Stop()
	s.prune(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.runSchedules(ctx)
			s.nudge() // waiting runs whose time has come
		case <-hour.C:
			s.prune(ctx)
			s.hookMu.Lock()
			s.hookHit = map[string][]time.Time{}
			s.hookMu.Unlock()
		}
	}
}

// prune keeps each person's run history for as long as they chose, and events for 30 days.
func (s *Server) prune(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		st, _ := s.store.GetSettings(ctx, uid)
		_ = s.store.PruneRuns(ctx, uid, st.KeepRunsDays)
		_ = s.store.PruneEvents(ctx, uid, 30)
	}
}

// ---- Pocket Core ----

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh; each
// time it also reports how everybody connects (modes.go).
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

// eventTopics are what Flows publishes on the Core bus.
var eventTopics = []string{"flows.run_finished", "flows.run_failed", "flows.flow_created"}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "flows", Name: "Pocket Flows",
		Description: "Automations for your Pocket apps: when something happens (a price drops, a document arrives, a task is done, a time of day, a webhook), do things for you — send a notification, add a task, a note, a calendar event or a transaction, call a web address, ask AI. Recipes to start from, a run history with every step, and an n8n import.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "FLOWS",
		Publishes: eventTopics,
		Offers:    linkOffers,
		Uses:      append(linkUses(), core.InferenceUse()),
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.oidcCallbackURL()}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// resolveSibling asks Core where a sibling app lives (cached a minute).
func (s *Server) resolveSibling(ctx context.Context, slug string) string {
	if !s.core.Configured() {
		return ""
	}
	s.coreMu.Lock()
	if r, ok := s.resolved[slug]; ok && time.Since(r.at) < time.Minute {
		s.coreMu.Unlock()
		return r.url
	}
	s.coreMu.Unlock()
	url := ""
	if res, err := s.core.Resolve(ctx, slug); err == nil {
		url = res.URL
	}
	s.coreMu.Lock()
	s.resolved[slug] = resolvedURL{url: url, at: time.Now()}
	s.coreMu.Unlock()
	return url
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// publish sends an event for a person (fire and forget, through the outbox).
func (s *Server) publish(ctx context.Context, uid int64, topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		payload["user_email"] = u.Email
	}
	if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
		s.log.Debug("pocket core: publish failed", "topic", topic, "err", err)
	}
}
