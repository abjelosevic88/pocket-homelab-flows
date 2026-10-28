package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// ---- sibling Pocket apps ----
//
// Flows hears a person's events from their Pocket apps through Pocket Core
// (an events:read link per app) and acts in them through Core links too:
// Flows asks Core for a five-minute token acting as that person, with only
// the scopes the step needs, never kept, so a link switched off in Core
// applies at once. For acting, a person may instead connect an app directly
// with their own URL and API token (Settings → Connected apps → Advanced).

const scopeEventsRead = "events:read"

// pocketApp is one sibling Pocket app Flows listens to and/or acts in.
type pocketApp struct {
	slug, name string
	scopes     []string
	reason     string
	events     bool // Flows listens to its events
	actions    bool // flows can act in it
}

// pocketApps is every sibling, from the catalog: events:read for each app
// with events, plus the scopes its steps and pickers need.
var pocketApps = func() []pocketApp {
	acts := catalog.ActionApps()
	out := []pocketApp{}
	seen := map[string]bool{}
	add := func(slug, name string) {
		if seen[slug] {
			return
		}
		seen[slug] = true
		a := pocketApp{slug: slug, name: name}
		if app, ok := catalog.AppBySlug(slug); ok && len(app.Events) > 0 {
			a.events = true
			a.scopes = append(a.scopes, scopeEventsRead)
		}
		if sc := acts[slug]; len(sc) > 0 {
			a.actions = true
			a.scopes = append(a.scopes, sc...)
		}
		switch {
		case a.events && a.actions:
			a.reason = "Start your flows when something happens in " + name + ", and let your flows add things there"
		case a.actions:
			a.reason = "Let your flows add things in " + name
		default:
			a.reason = "Start your flows when something happens in " + name
		}
		out = append(out, a)
	}
	for _, a := range catalog.Apps {
		add(a.Slug, a.Name)
	}
	for slug := range acts {
		add(slug, "Pocket "+strings.ToUpper(slug[:1])+slug[1:])
	}
	return out
}()

func appBySlug(slug string) (pocketApp, bool) {
	for _, a := range pocketApps {
		if a.slug == slug {
			return a, true
		}
	}
	return pocketApp{}, false
}

// linkUses is the manifest's Uses.
func linkUses() []core.Use {
	out := make([]core.Use, 0, len(pocketApps))
	for _, a := range pocketApps {
		out = append(out, core.Use{App: a.slug, Scopes: a.scopes, Reason: a.reason})
	}
	return out
}

// Connection modes, chosen per person and per app in Settings.
const (
	modeCore   = "core"
	modeDirect = "direct"
	modeOff    = "off"
)

// connMode is the mode in effect: the chosen one, else direct when a token
// was entered, else Pocket Core when Flows is connected to one, else direct.
func (s *Server) connMode(mode, token string) string {
	switch mode {
	case modeCore, modeDirect, modeOff:
		return mode
	}
	if token != "" || !s.core.Configured() {
		return modeDirect
	}
	return modeCore
}

// notConnected means the person has no working connection to the app.
type notConnected struct{ msg string }

func (e *notConnected) Error() string { return e.msg }

func isNotConnected(err error) bool {
	var nc *notConnected
	return errors.As(err, &nc)
}

// siblingConn is how Flows reaches one person's copy of a sibling app.
type siblingConn struct {
	client *pocket.Client
	via    string
	public string
}

func linkReason(app, reason string) string {
	switch reason {
	case "off_person":
		return "you switched the Flows → " + app + " link off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "the Flows → " + app + " link is switched off in Pocket Core (Links)"
	case "target_missing":
		return app + " is not registered with Pocket Core"
	case "not_offered":
		return "this " + app + " version does not accept this link yet"
	case "not_declared":
		return "Pocket Core has not seen this Flows version yet; it re-registers within a minute of starting"
	case "not_in_audience":
		return "you do not have access to " + app + " in Pocket Core"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	}
	return ""
}

// connSettings returns a person's saved connection to an app.
func connSettings(st store.Settings, slug string) store.Conn {
	return st.Connections[slug]
}

// siblingFor connects to app a for person u with exactly these scopes.
func (s *Server) siblingFor(ctx context.Context, u *store.User, st store.Settings, a pocketApp, scopes ...string) (*siblingConn, error) {
	if u == nil {
		return nil, &notConnected{"nobody to act for"}
	}
	c := connSettings(st, a.slug)
	switch s.connMode(c.Mode, c.Token) {
	case modeOff:
		return nil, &notConnected{a.name + " is switched off in Settings → Connected apps"}
	case modeDirect:
		if c.Token == "" {
			return nil, &notConnected{a.name + " is not set up (Settings → Connected apps): add a URL and token, or switch to Pocket Core"}
		}
		base := c.URL
		if base == "" {
			base = s.resolveSibling(ctx, a.slug)
		}
		if base == "" {
			return nil, &notConnected{"set the " + a.name + " URL in Settings → Connected apps"}
		}
		return &siblingConn{client: pocket.New(base, c.Token, 30*time.Second), via: modeDirect, public: firstNonEmpty(c.PublicURL, base)}, nil
	}
	if !s.core.Configured() {
		return nil, &notConnected{a.name + " through Pocket Core: there is no Pocket Core on this box; use a direct connection"}
	}
	tok, err := s.core.ActAs(ctx, u.Email, a.slug, scopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			if t := linkReason(a.name, le.Reason); t != "" {
				return nil, &notConnected{a.name + " is not set up: " + t}
			}
			return nil, &notConnected{a.name + " is not set up: " + le.Error()}
		}
		return nil, fmt.Errorf("%s through Pocket Core: %w", a.name, err)
	}
	if tok.URL == "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("%s through Pocket Core: Core gave no address or token", a.name)
	}
	return &siblingConn{client: pocket.New(tok.URL, tok.AccessToken, 30*time.Second), via: modeCore, public: firstNonEmpty(c.PublicURL, firstNonEmpty(tok.PublicURL, tok.URL))}, nil
}

// connStatus says, without issuing anything, how a person reaches an app.
// listen reports whether the events:read link is on.
func (s *Server) connStatus(ctx context.Context, u *store.User, st store.Settings, a pocketApp, state map[string]core.LinkState) (eff, via, detail string, listen bool) {
	c := connSettings(st, a.slug)
	eff = s.connMode(c.Mode, c.Token)
	if l, ok := state[scopeEventsRead]; ok && l.On {
		listen = true
	}
	if eff == modeOff {
		return eff, "", "switched off", false
	}
	if eff == modeDirect && a.actions {
		if c.Token == "" {
			return eff, "", "no token yet", listen
		}
		return eff, modeDirect, "", listen
	}
	if u == nil || !s.core.Configured() {
		return eff, "", "there is no Pocket Core on this box", false
	}
	if state == nil {
		return eff, "", "Pocket Core did not answer", false
	}
	var on, off []string
	reason := ""
	for _, sc := range a.scopes {
		l, ok := state[sc]
		if ok && l.On {
			on = append(on, sc)
			continue
		}
		off = append(off, sc)
		if reason == "" {
			reason = "not_declared"
			if ok {
				reason = l.State
			}
		}
	}
	text := linkReason(a.name, reason)
	if text == "" {
		text = reason
	}
	switch {
	case len(off) == 0:
		return eff, modeCore, "", listen
	case len(on) == 0:
		return eff, "", text, listen
	}
	sort.Strings(off)
	return eff, modeCore, "partly: " + strings.Join(off, ", ") + " off (" + text + ")", listen
}

// linkStates asks Core for every link of a person, by app and scope.
func (s *Server) linkStates(ctx context.Context, email string) map[string]map[string]core.LinkState {
	out := map[string]map[string]core.LinkState{}
	if !s.core.Configured() || email == "" {
		return out
	}
	list, err := s.core.LinkStatus(ctx, email, "")
	if err != nil {
		return nil
	}
	for _, l := range list {
		if out[l.App] == nil {
			out[l.App] = map[string]core.LinkState{}
		}
		out[l.App][l.Scope] = l
	}
	return out
}
