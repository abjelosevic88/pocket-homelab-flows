package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const flowCols = `id, user_id, name, description, enabled, trigger_kind, trigger, steps, COALESCE(hook_token, ''), next_run_at,
	last_run_at, last_status, run_count, fail_count, recipe, source, external_id, created_at, updated_at`

func scanFlow(row interface{ Scan(...any) error }) (*Flow, error) {
	var f Flow
	var enabled int
	var trig, steps string
	if err := row.Scan(&f.ID, &f.UserID, &f.Name, &f.Description, &enabled, &f.TriggerKind, &trig, &steps, &f.HookToken, &f.NextRunAt,
		&f.LastRunAt, &f.LastStatus, &f.RunCount, &f.FailCount, &f.Recipe, &f.Source, &f.ExternalID, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	f.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(trig), &f.Trigger)
	_ = json.Unmarshal([]byte(steps), &f.Steps)
	if f.Steps == nil {
		f.Steps = []Step{}
	}
	for i := range f.Steps {
		if f.Steps[i].Config == nil {
			f.Steps[i].Config = map[string]string{}
		}
	}
	return &f, nil
}

func collectFlows(rows *sql.Rows, err error) ([]Flow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Flow{}
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func hookValue(t string) any {
	if t == "" {
		return nil
	}
	return t
}

// CreateFlow adds a flow for a person.
func (s *Store) CreateFlow(ctx context.Context, uid int64, f *Flow) error {
	if f.Steps == nil {
		f.Steps = []Step{}
	}
	if f.Source == "" {
		f.Source = "app"
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO flows (user_id, name, description, enabled, trigger_kind, trigger, steps, hook_token, next_run_at, recipe, source, external_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, f.Name, f.Description, boolInt(f.Enabled), f.TriggerKind, jsonText(f.Trigger), jsonText(f.Steps), hookValue(f.HookToken), f.NextRunAt, f.Recipe, f.Source, f.ExternalID, now, now)
	if err != nil {
		return conflict(err)
	}
	f.ID, _ = res.LastInsertId()
	f.UserID, f.CreatedAt, f.UpdatedAt = uid, now, now
	return nil
}

// UpdateFlow saves a flow's definition (not its run counters).
func (s *Store) UpdateFlow(ctx context.Context, uid int64, f *Flow) error {
	f.UpdatedAt = Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE flows SET name = ?, description = ?, enabled = ?, trigger_kind = ?, trigger = ?, steps = ?, hook_token = ?, next_run_at = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		f.Name, f.Description, boolInt(f.Enabled), f.TriggerKind, jsonText(f.Trigger), jsonText(f.Steps), hookValue(f.HookToken), f.NextRunAt, f.UpdatedAt, f.ID, uid)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetFlow returns one of a person's flows.
func (s *Store) GetFlow(ctx context.Context, uid, id int64) (*Flow, error) {
	return scanFlow(s.DB.QueryRowContext(ctx, `SELECT `+flowCols+` FROM flows WHERE id = ? AND user_id = ?`, id, uid))
}

// FlowByHook finds the flow behind a webhook address (any person: the token is the secret).
func (s *Store) FlowByHook(ctx context.Context, token string) (*Flow, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanFlow(s.DB.QueryRowContext(ctx, `SELECT `+flowCols+` FROM flows WHERE hook_token = ?`, token))
}

// ListFlows lists a person's flows, newest first.
func (s *Store) ListFlows(ctx context.Context, uid int64) ([]Flow, error) {
	return collectFlows(s.DB.QueryContext(ctx, `SELECT `+flowCols+` FROM flows WHERE user_id = ? ORDER BY enabled DESC, name COLLATE NOCASE, id`, uid))
}

// EventFlows lists a person's enabled event flows.
func (s *Store) EventFlows(ctx context.Context, uid int64) ([]Flow, error) {
	return collectFlows(s.DB.QueryContext(ctx, `SELECT `+flowCols+` FROM flows WHERE user_id = ? AND enabled = 1 AND trigger_kind = 'event' ORDER BY id`, uid))
}

// EventUsers lists the people with at least one enabled event flow.
func (s *Store) EventUsers(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT user_id FROM flows WHERE enabled = 1 AND trigger_kind = 'event'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// DueFlows lists enabled schedule flows whose next run time has come (every person).
func (s *Store) DueFlows(ctx context.Context, now string) ([]Flow, error) {
	return collectFlows(s.DB.QueryContext(ctx, `SELECT `+flowCols+` FROM flows WHERE trigger_kind = 'schedule' AND enabled = 1 AND next_run_at != '' AND next_run_at <= ? ORDER BY next_run_at LIMIT 200`, now))
}

// SetNextRun stores a schedule flow's next run time.
func (s *Store) SetNextRun(ctx context.Context, uid, id int64, next string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE flows SET next_run_at = ? WHERE id = ? AND user_id = ?`, next, id, uid)
	return err
}

// SetFlowEnabled switches a flow on or off.
func (s *Store) SetFlowEnabled(ctx context.Context, uid, id int64, on bool, next string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE flows SET enabled = ?, next_run_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`, boolInt(on), next, Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFlow removes a flow and its runs.
func (s *Store) DeleteFlow(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM flows WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FlowFinished records the outcome of a run on its flow.
func (s *Store) FlowFinished(ctx context.Context, uid, id int64, status, at string) error {
	failed := 0
	if status == RunFailed {
		failed = 1
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE flows SET last_run_at = ?, last_status = ?, run_count = run_count + 1, fail_count = fail_count + ? WHERE id = ? AND user_id = ?`, at, status, failed, id, uid)
	return err
}

// FlowByExternal finds a flow by its import identity (for idempotent imports).
func (s *Store) FlowByExternal(ctx context.Context, uid int64, externalID string) (*Flow, error) {
	if strings.TrimSpace(externalID) == "" {
		return nil, ErrNotFound
	}
	return scanFlow(s.DB.QueryRowContext(ctx, `SELECT `+flowCols+` FROM flows WHERE user_id = ? AND external_id = ?`, uid, externalID))
}
