package store

import "context"

// InsertRun writes a finished run as it is (demo data and tests).
func (s *Store) InsertRun(ctx context.Context, r *Run) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO runs (user_id, flow_id, flow_name, trigger_kind, trigger_ref, summary, input, status, step, attempt, outputs, log, error, resume_at, started_at, finished_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UserID, r.FlowID, r.FlowName, r.TriggerKind, r.TriggerRef, r.Summary, jsonText(r.Input), r.Status, r.Step, r.Attempt, jsonText(r.Outputs), jsonText(r.Log),
		r.Error, r.ResumeAt, r.StartedAt, r.FinishedAt, r.CreatedAt)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// InsertEvent writes an event with its own time (demo data and tests).
func (s *Store) InsertEvent(ctx context.Context, e *Event) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO events (user_id, core_id, topic, source, subject, payload, matched, received_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.CoreID, e.Topic, e.Source, e.Subject, jsonText(e.Payload), e.Matched, e.ReceivedAt)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// SetFlowStats sets a flow's counters as they are (demo data).
func (s *Store) SetFlowStats(ctx context.Context, uid, id int64, runs, fails int, lastAt, lastStatus string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE flows SET run_count = ?, fail_count = ?, last_run_at = ?, last_status = ? WHERE id = ? AND user_id = ?`, runs, fails, lastAt, lastStatus, id, uid)
	return err
}
