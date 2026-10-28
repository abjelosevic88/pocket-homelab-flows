package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const runCols = `id, user_id, flow_id, flow_name, trigger_kind, trigger_ref, summary, input, status, step, attempt, outputs, log, error, resume_at, started_at, finished_at, created_at`

// runListCols leaves out the big columns for lists.
const runListCols = `id, user_id, flow_id, flow_name, trigger_kind, trigger_ref, summary, '{}', status, step, attempt, '{}', '[]', error, resume_at, started_at, finished_at, created_at`

func scanRun(row interface{ Scan(...any) error }) (*Run, error) {
	var r Run
	var input, outputs, log string
	if err := row.Scan(&r.ID, &r.UserID, &r.FlowID, &r.FlowName, &r.TriggerKind, &r.TriggerRef, &r.Summary, &input, &r.Status, &r.Step, &r.Attempt,
		&outputs, &log, &r.Error, &r.ResumeAt, &r.StartedAt, &r.FinishedAt, &r.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(input), &r.Input)
	_ = json.Unmarshal([]byte(outputs), &r.Outputs)
	_ = json.Unmarshal([]byte(log), &r.Log)
	if r.Input == nil {
		r.Input = map[string]any{}
	}
	if r.Outputs == nil {
		r.Outputs = map[string]json.RawMessage{}
	}
	if r.Log == nil {
		r.Log = []LogEntry{}
	}
	return &r, nil
}

func collectRuns(rows *sql.Rows, err error) ([]Run, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// EnqueueRun adds a queued run. A run with the same flow and trigger_ref
// already there means the trigger was seen before: (nil, false, nil).
func (s *Store) EnqueueRun(ctx context.Context, f *Flow, kind, ref, summary string, input map[string]any) (*Run, bool, error) {
	if input == nil {
		input = map[string]any{}
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO runs (user_id, flow_id, flow_name, trigger_kind, trigger_ref, summary, input, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?) ON CONFLICT DO NOTHING`, f.UserID, f.ID, f.Name, kind, ref, summary, jsonText(input), now)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, false, nil
	}
	id, _ := res.LastInsertId()
	return &Run{ID: id, UserID: f.UserID, FlowID: f.ID, FlowName: f.Name, TriggerKind: kind, TriggerRef: ref, Summary: summary, Input: input,
		Status: RunQueued, Outputs: map[string]json.RawMessage{}, Log: []LogEntry{}, CreatedAt: now}, true, nil
}

// RecentRuns counts a flow's runs since a time (the runaway guard).
func (s *Store) RecentRuns(ctx context.Context, flowID int64, since string) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM runs WHERE flow_id = ? AND created_at >= ?`, flowID, since).Scan(&n)
	return n
}

// ClaimRuns marks up to n runs that are ready (queued, or waiting whose time
// has come) as running and returns them, oldest first.
func (s *Store) ClaimRuns(ctx context.Context, now string, n int) ([]Run, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	rows, err := tx.QueryContext(ctx, `SELECT `+runCols+` FROM runs WHERE status = 'queued' OR (status = 'waiting' AND resume_at <= ?) ORDER BY id LIMIT ?`, now, n)
	runs, err := collectRuns(rows, err)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		started := runs[i].StartedAt
		if started == "" {
			started = now
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET status = 'running', started_at = ? WHERE id = ?`, started, runs[i].ID); err != nil {
			return nil, err
		}
		runs[i].Status, runs[i].StartedAt = RunRunning, started
	}
	return runs, tx.Commit()
}

// RequeueRunning puts runs a crash left "running" back in the queue.
func (s *Store) RequeueRunning(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE runs SET status = 'queued' WHERE status = 'running'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SaveRunProgress stores a run's state after a step.
func (s *Store) SaveRunProgress(ctx context.Context, r *Run) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE runs SET status = ?, step = ?, attempt = ?, outputs = ?, log = ?, error = ?, resume_at = ?, finished_at = ? WHERE id = ? AND user_id = ?`,
		r.Status, r.Step, r.Attempt, jsonText(r.Outputs), jsonText(r.Log), r.Error, r.ResumeAt, r.FinishedAt, r.ID, r.UserID)
	return err
}

// GetRun returns one of a person's runs.
func (s *Store) GetRun(ctx context.Context, uid, id int64) (*Run, error) {
	return scanRun(s.DB.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id = ? AND user_id = ?`, id, uid))
}

// RunFilter narrows the run history.
type RunFilter struct {
	FlowID int64
	Status string // "" | ok | failed | active (queued, running, waiting) | stopped
	Before int64  // id, for paging
	Limit  int
}

// ListRuns lists a person's runs, newest first.
func (s *Store) ListRuns(ctx context.Context, uid int64, f RunFilter) ([]Run, error) {
	q := `SELECT ` + runListCols + ` FROM runs WHERE user_id = ?`
	args := []any{uid}
	if f.FlowID > 0 {
		q += ` AND flow_id = ?`
		args = append(args, f.FlowID)
	}
	switch f.Status {
	case "":
	case "active":
		q += ` AND status IN ('queued', 'running', 'waiting')`
	default:
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Before > 0 {
		q += ` AND id < ?`
		args = append(args, f.Before)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)
	return collectRuns(s.DB.QueryContext(ctx, q, args...))
}

// CancelRun stops a run that has not finished.
func (s *Store) CancelRun(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE runs SET status = 'cancelled', resume_at = '', finished_at = ? WHERE id = ? AND user_id = ? AND status IN ('queued', 'waiting')`, Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetRun(ctx, uid, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// RetryRun queues a failed run again from the step that failed.
func (s *Store) RetryRun(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE runs SET status = 'queued', attempt = 0, error = '', resume_at = '', finished_at = '' WHERE id = ? AND user_id = ? AND status IN ('failed', 'cancelled')`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetRun(ctx, uid, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// RunStats are a person's numbers for the sidebar and the overview.
type RunStats struct {
	Flows     int `json:"flows"`
	Enabled   int `json:"enabled"`
	Today     int `json:"runs_today"`
	FailedDay int `json:"failed_today"`
	Week      int `json:"runs_week"`
	FailedWk  int `json:"failed_week"`
	Active    int `json:"active"`
	Events    int `json:"events_today"`
}

// Stats counts a person's flows and runs; dayStart and weekStart are UTC timestamps.
func (s *Store) Stats(ctx context.Context, uid int64, dayStart, weekStart string) RunStats {
	var st RunStats
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(enabled), 0) FROM flows WHERE user_id = ?`, uid).Scan(&st.Flows, &st.Enabled)
	_ = s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(created_at >= ?), 0), COALESCE(SUM(created_at >= ? AND status = 'failed'), 0),
		COUNT(1), COALESCE(SUM(status = 'failed'), 0), COALESCE(SUM(status IN ('queued', 'running', 'waiting')), 0)
		FROM runs WHERE user_id = ? AND (created_at >= ? OR status IN ('queued', 'running', 'waiting'))`, dayStart, dayStart, uid, weekStart).Scan(&st.Today, &st.FailedDay, &st.Week, &st.FailedWk, &st.Active)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM events WHERE user_id = ? AND received_at >= ?`, uid, dayStart).Scan(&st.Events)
	return st
}

// DayCount is runs on one day.
type DayCount struct {
	Day    string `json:"day"`
	OK     int    `json:"ok"`
	Failed int    `json:"failed"`
}

// RunsByDay counts a person's finished runs per local day (offset in minutes east of UTC).
func (s *Store) RunsByDay(ctx context.Context, uid int64, since string, offsetMin int) ([]DayCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date(created_at, ? || ' minutes') AS d, SUM(status != 'failed'), SUM(status = 'failed')
		FROM runs WHERE user_id = ? AND created_at >= ? AND status IN ('ok', 'failed', 'stopped') GROUP BY d ORDER BY d`, offsetMin, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayCount{}
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.OK, &d.Failed); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PruneRuns deletes a person's finished runs older than days.
func (s *Store) PruneRuns(ctx context.Context, uid int64, days int) error {
	if days <= 0 {
		return nil
	}
	cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM runs WHERE user_id = ? AND created_at < ? AND status IN ('ok', 'failed', 'stopped', 'cancelled')`, uid, cut)
	return err
}

// ---- events ----

const eventCols = `id, user_id, core_id, topic, source, subject, payload, matched, received_at`

func scanEvent(row interface{ Scan(...any) error }) (*Event, error) {
	var e Event
	var payload string
	if err := row.Scan(&e.ID, &e.UserID, &e.CoreID, &e.Topic, &e.Source, &e.Subject, &payload, &e.Matched, &e.ReceivedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(payload), &e.Payload)
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	return &e, nil
}

// AddEvent records a person's event; (false) when the Core id was seen before.
func (s *Store) AddEvent(ctx context.Context, uid int64, e *Event) (bool, error) {
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	e.ReceivedAt = Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO events (user_id, core_id, topic, source, subject, payload, received_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		uid, e.CoreID, e.Topic, e.Source, e.Subject, jsonText(e.Payload), e.ReceivedAt)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	e.ID, _ = res.LastInsertId()
	e.UserID = uid
	return true, nil
}

// SetEventMatched stores how many flows an event started.
func (s *Store) SetEventMatched(ctx context.Context, uid, id int64, n int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE events SET matched = ? WHERE id = ? AND user_id = ?`, n, id, uid)
	return err
}

// ListEvents lists a person's events, newest first; topic may end in "*".
func (s *Store) ListEvents(ctx context.Context, uid int64, topic string, before int64, limit int) ([]Event, error) {
	q := `SELECT ` + eventCols + ` FROM events WHERE user_id = ?`
	args := []any{uid}
	if topic != "" {
		if strings.HasSuffix(topic, "*") {
			q += ` AND topic LIKE ? || '%'`
			args = append(args, strings.TrimSuffix(topic, "*"))
		} else {
			q += ` AND topic = ?`
			args = append(args, topic)
		}
	}
	if before > 0 {
		q += ` AND id < ?`
		args = append(args, before)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// GetEvent returns one of a person's events.
func (s *Store) GetEvent(ctx context.Context, uid, id int64) (*Event, error) {
	return scanEvent(s.DB.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id = ? AND user_id = ?`, id, uid))
}

// TopicCount is how often a person has seen a topic.
type TopicCount struct {
	Topic string `json:"topic"`
	Count int    `json:"count"`
	Last  string `json:"last"`
}

// SeenTopics lists the topics a person's events had.
func (s *Store) SeenTopics(ctx context.Context, uid int64) ([]TopicCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT topic, COUNT(1), MAX(received_at) FROM events WHERE user_id = ? GROUP BY topic ORDER BY MAX(received_at) DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicCount{}
	for rows.Next() {
		var t TopicCount
		if err := rows.Scan(&t.Topic, &t.Count, &t.Last); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PruneEvents keeps a person's events for days (and at most 2,000).
func (s *Store) PruneEvents(ctx context.Context, uid int64, days int) error {
	cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM events WHERE user_id = ? AND received_at < ?`, uid, cut); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM events WHERE user_id = ? AND id NOT IN (SELECT id FROM events WHERE user_id = ? ORDER BY id DESC LIMIT 2000)`, uid, uid)
	return err
}
