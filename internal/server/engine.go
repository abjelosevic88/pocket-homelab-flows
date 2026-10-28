package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/catalog"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/flow"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// ---- the flow engine ----
//
// A trigger queues a run (store.EnqueueRun, idempotent per trigger_ref).
// Workers claim runs and do the flow's steps in order, saving the step
// index and every step's output as they go, so a restart picks a run up at
// the step it was on. A "Wait" step parks the run until its time; a step
// that fails for a reason that may pass (the other app is down, a 5xx) is
// tried again after 1, 5 and 20 minutes; anything else fails the run (or
// is noted and skipped when the step says "continue on error").

// Limits that keep a flow from running away.
const (
	maxRunsPerHour = 500
	stepTimeout    = 3 * time.Minute
	maxWait        = 30 * 24 * time.Hour
)

var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 20 * time.Minute}

// nudge wakes a worker.
func (s *Server) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// enqueue queues a run of f; (nil, nil) when that trigger was handled before.
func (s *Server) enqueue(ctx context.Context, f *store.Flow, kind, ref, summary string, input map[string]any) (*store.Run, error) {
	since := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if s.store.RecentRuns(ctx, f.ID, since) >= maxRunsPerHour {
		s.log.Warn("flow ran too often this hour, run skipped", "flow", f.ID, "trigger", ref)
		return nil, fmt.Errorf("this flow already ran %d times in the last hour", maxRunsPerHour)
	}
	r, fresh, err := s.store.EnqueueRun(ctx, f, kind, ref, summary, input)
	if err != nil || !fresh {
		return nil, err
	}
	s.nudge()
	return r, nil
}

// worker claims and does runs until ctx ends.
func (s *Server) worker(ctx context.Context) {
	idle := time.NewTimer(time.Second)
	defer idle.Stop()
	for {
		runs, err := s.store.ClaimRuns(ctx, store.Now(), 1)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("claim runs", "err", err)
		}
		if len(runs) == 1 {
			s.nudge() // there may be more: let another worker look
			s.execute(ctx, &runs[0])
			continue
		}
		idle.Reset(15 * time.Second)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-idle.C:
		}
	}
}

// stepErr classifies a failure.
type stepErr struct {
	msg   string
	retry bool
}

func (e *stepErr) Error() string { return e.msg }

func permanent(format string, a ...any) error { return &stepErr{msg: fmt.Sprintf(format, a...)} }

// retryable reports whether trying the step again later may work.
func retryable(err error) bool {
	var se *stepErr
	if errors.As(err, &se) {
		return se.retry
	}
	if isNotConnected(err) {
		return false
	}
	var pe *pocket.StatusError
	if errors.As(err, &pe) {
		return pe.Temporary()
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "EOF")
}

// vars is everything a step's placeholders can use.
func (s *Server) vars(r *store.Run, f *store.Flow, u *store.User) flow.Vars {
	now := time.Now()
	steps := map[string]any{}
	for id, raw := range r.Outputs {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			steps[id] = v
		}
	}
	trigger := map[string]any{}
	for k, v := range r.Input {
		trigger[k] = v
	}
	me := map[string]any{}
	if u != nil {
		me["name"], me["email"] = u.Name, u.Email
	}
	return flow.Vars{
		"trigger": trigger, "steps": steps, "me": me,
		"flow": map[string]any{"id": f.ID, "name": f.Name, "run": r.ID, "url": s.cfg.BaseURL + "/runs/" + strconv.FormatInt(r.ID, 10)},
		"now":  now.Format(time.RFC3339), "today": now.Format("2006-01-02"), "time": now.Format("15:04"),
		"tomorrow": now.AddDate(0, 0, 1).Format("2006-01-02"), "weekday": now.Weekday().String(),
	}
}

// execute does a claimed run from its current step.
func (s *Server) execute(ctx context.Context, r *store.Run) {
	f, err := s.store.GetFlow(ctx, r.UserID, r.FlowID)
	if err != nil {
		r.Status, r.Error, r.FinishedAt = store.RunCancelled, "the flow was deleted", store.Now()
		_ = s.store.SaveRunProgress(ctx, r)
		return
	}
	u, _ := s.store.GetUser(ctx, r.UserID)
	st, _ := s.store.GetSettings(ctx, r.UserID)
	v := s.vars(r, f, u)
	for i := r.Step; i < len(f.Steps); i++ {
		if ctx.Err() != nil {
			return // shutting down: RequeueRunning picks it up on start
		}
		step := f.Steps[i]
		started := time.Now()
		entry := store.LogEntry{Step: i + 1, StepID: step.ID, Kind: step.Kind, Name: stepName(step), At: store.Now()}
		done := func(status, msg string) {
			entry.Status, entry.Message, entry.MS = status, msg, time.Since(started).Milliseconds()
			r.Log = append(r.Log, entry)
		}
		if step.Kind == "filter" {
			if !flow.CheckAll(step.OnlyIf, step.Match, v) {
				done("stopped", "Conditions not met: "+describeAll(step.OnlyIf, step.Match))
				r.Step = i + 1
				s.finish(ctx, r, f, st, store.RunStopped)
				return
			}
			done("ok", "Conditions met")
			r.Step, r.Attempt = i+1, 0
			_ = s.store.SaveRunProgress(ctx, r)
			continue
		}
		if !flow.CheckAll(step.OnlyIf, step.Match, v) {
			done("skipped", "Skipped: "+describeAll(step.OnlyIf, step.Match)+" did not hold")
			r.Step, r.Attempt = i+1, 0
			_ = s.store.SaveRunProgress(ctx, r)
			continue
		}
		if step.Kind == "wait" {
			d, err := waitFor(flow.RenderAll(step.Config, v))
			if err != nil {
				done("failed", err.Error())
				r.Error = err.Error()
				s.finish(ctx, r, f, st, store.RunFailed)
				return
			}
			done("waiting", "Waiting "+humanDuration(d))
			r.Step, r.Attempt = i+1, 0
			r.Status, r.ResumeAt = store.RunWaiting, time.Now().UTC().Add(d).Format(time.RFC3339Nano)
			_ = s.store.SaveRunProgress(ctx, r)
			return
		}
		sctx, cancel := context.WithTimeout(ctx, stepTimeout)
		out, msg, err := s.doStep(sctx, r, f, step, flow.RenderAll(step.Config, v), v, u, st)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if retryable(err) && r.Attempt < len(retryAfter) {
				wait := retryAfter[r.Attempt]
				done("retry", fmt.Sprintf("%s — trying again in %s", err.Error(), humanDuration(wait)))
				r.Attempt++
				r.Status, r.ResumeAt = store.RunWaiting, time.Now().UTC().Add(wait).Format(time.RFC3339Nano)
				_ = s.store.SaveRunProgress(ctx, r)
				return
			}
			if step.OnError == "continue" {
				done("failed", err.Error()+" (carried on)")
				r.Step, r.Attempt = i+1, 0
				_ = s.store.SaveRunProgress(ctx, r)
				continue
			}
			done("failed", err.Error())
			r.Error = stepName(step) + ": " + err.Error()
			s.finish(ctx, r, f, st, store.RunFailed)
			return
		}
		if out == nil {
			out = map[string]any{}
		}
		if b, err := json.Marshal(out); err == nil {
			if len(b) > 256<<10 {
				b, _ = json.Marshal(map[string]any{"note": "output too large to keep"})
			}
			r.Outputs[step.ID] = b
		}
		v["steps"].(map[string]any)[step.ID] = out
		done("ok", msg)
		r.Step, r.Attempt, r.ResumeAt = i+1, 0, ""
		_ = s.store.SaveRunProgress(ctx, r)
	}
	s.finish(ctx, r, f, st, store.RunOK)
}

// finish closes a run, updates its flow and tells the person when it matters.
func (s *Server) finish(ctx context.Context, r *store.Run, f *store.Flow, st store.Settings, status string) {
	r.Status, r.FinishedAt, r.ResumeAt = status, store.Now(), ""
	if err := s.store.SaveRunProgress(ctx, r); err != nil {
		s.log.Warn("save run", "run", r.ID, "err", err)
	}
	wasFailing := f.LastStatus == store.RunFailed
	_ = s.store.FlowFinished(ctx, r.UserID, f.ID, status, r.FinishedAt)
	s.ran.Add(1)
	payload := map[string]any{"run_id": r.ID, "flow_id": f.ID, "flow": f.Name, "status": status, "trigger": r.TriggerKind, "summary": r.Summary, "steps": len(r.Log), "url": s.cfg.BaseURL + "/runs/" + strconv.FormatInt(r.ID, 10)}
	if status == store.RunFailed {
		s.failed.Add(1)
		payload["error"] = r.Error
		s.publish(ctx, r.UserID, "flows.run_failed", fmt.Sprintf("run:%d", r.ID), payload)
		if st.NotifyFailures && st.NotifyURL != "" {
			s.sendNotice(ctx, st, notify.Message{Title: "Flow failed: " + f.Name, Body: r.Error, Priority: "failure", Tags: []string{"warning"}, Link: s.runLink(r.ID)})
		}
		return
	}
	s.publish(ctx, r.UserID, "flows.run_finished", fmt.Sprintf("run:%d", r.ID), payload)
	if wasFailing && status == store.RunOK && st.NotifyRecover && st.NotifyURL != "" {
		s.sendNotice(ctx, st, notify.Message{Title: "Flow works again: " + f.Name, Body: "The last run went through.", Priority: "success", Tags: []string{"white_check_mark"}, Link: s.runLink(r.ID)})
	}
}

func (s *Server) runLink(id int64) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return s.cfg.BaseURL + "/runs/" + strconv.FormatInt(id, 10)
}

func (s *Server) sendNotice(ctx context.Context, st store.Settings, m notify.Message) {
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(c, m); err != nil {
		s.log.Warn("notify", "err", err)
	}
}

func stepName(st store.Step) string {
	if st.Name != "" {
		return st.Name
	}
	if k, ok := catalog.StepByKind(st.Kind); ok {
		return k.Name
	}
	return st.Kind
}

func describeAll(cs []store.Condition, match string) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, flow.Describe(c))
	}
	join := " and "
	if match == "any" {
		join = " or "
	}
	return strings.Join(parts, join)
}

// waitFor reads a Wait step's length.
func waitFor(cfg map[string]string) (time.Duration, error) {
	n, ok := flow.Number(cfg["amount"])
	if !ok || n <= 0 {
		return 0, permanent("how long to wait must be a number above 0")
	}
	unit := time.Minute
	switch cfg["unit"] {
	case "hours":
		unit = time.Hour
	case "days":
		unit = 24 * time.Hour
	}
	d := time.Duration(n * float64(unit))
	if d > maxWait {
		return 0, permanent("a wait can be at most 30 days")
	}
	return d, nil
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		n := int(d / (24 * time.Hour))
		return plural(n, "day")
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int(d/time.Hour), "hour")
	case d >= time.Minute:
		return plural(int(d.Round(time.Minute)/time.Minute), "minute")
	}
	return plural(int(d.Round(time.Second)/time.Second), "second")
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return strconv.Itoa(n) + " " + w + "s"
}
