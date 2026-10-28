package server

import (
	"context"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// ---- connection modes on Pocket Core's map ----
//
// Core can switch its own links but cannot see a URL and token pasted into
// Flows, so Flows tells it how each person actually connects to each Pocket
// app it acts in: "core", "direct" or "off", the mode in effect (what
// Settings shows, defaults included). Reported after each registration (on
// start and every 30 minutes) and right after a person changes a connection;
// one batched call for everybody. Report only: failures are logged and
// nothing depends on them.

// connModes is every person's effective mode for every Pocket app.
func (s *Server) connModes(ctx context.Context) ([]core.ConnMode, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.ConnMode, 0, len(users)*len(pocketApps))
	for _, u := range users {
		if u.Email == "" {
			continue
		}
		st, err := s.store.GetSettings(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range pocketApps {
			c := connSettings(st, a.slug)
			mode := s.connMode(c.Mode, c.Token)
			if !a.actions && mode == modeDirect {
				mode = modeCore // events only ever come through Core
			}
			out = append(out, core.ConnMode{User: u.Email, Target: a.slug, Mode: mode})
		}
	}
	return out, nil
}

// reportModes sends the modes to Core when Flows is connected to one
// (registered, so it has an app token). Serialized, and computed at send
// time, so the last report always carries the latest modes.
func (s *Server) reportModes(ctx context.Context) {
	if !s.core.Configured() || s.core.Token() == "" {
		return
	}
	s.modesMu.Lock()
	defer s.modesMu.Unlock()
	modes, err := s.connModes(ctx)
	if err != nil {
		s.log.Warn("pocket core: reading connection modes failed", "err", err)
		return
	}
	if err := s.core.ReportModes(ctx, modes); err != nil {
		s.log.Warn("pocket core: reporting connection modes failed", "err", err)
		return
	}
	s.log.Debug("pocket core: connection modes reported", "count", len(modes))
}

// reportModesSoon reports in the background, so saving settings never waits on Core.
func (s *Server) reportModesSoon() {
	if !s.core.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.reportModes(ctx)
	}()
}
