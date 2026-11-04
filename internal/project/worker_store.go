package project

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const workerCols = `id, key, project_id, user_id, session_id, COALESCE(issue_id, ''),
	title, brief, role, status, error, COALESCE(review_json, ''), COALESCE(user_decision, ''),
	created_at, updated_at, started_at, finished_at`

// CreateWorker inserts a queued worker row.
func (s *Store) CreateWorker(ctx context.Context, userID, projectID string, in DelegateInput, sessionID string) (*Worker, error) {
	if err := ValidateWorkerTitle(in.Title); err != nil {
		return nil, err
	}
	role, err := NormalizeWorkerRole(in.Role)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	w := &Worker{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		UserID:    userID,
		SessionID: sessionID,
		IssueID:   strings.TrimSpace(in.IssueID),
		Title:     strings.TrimSpace(in.Title),
		Brief:     strings.TrimSpace(in.Brief),
		Role:      role,
		Status:    WorkerQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO project_workers (
			id, key, project_id, user_id, session_id, issue_id,
			title, brief, role, status, error, created_at, updated_at
		) VALUES (
			$1, 'WRK-' || nextval('project_worker_key_seq'), $2, $3, $4, NULLIF($5, ''),
			$6, $7, $8, $9, '', $10, $10
		) RETURNING key`,
		w.ID, projectID, userID, sessionID, w.IssueID, w.Title, w.Brief, w.Role, w.Status, now,
	).Scan(&w.Key)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Store) GetWorker(ctx context.Context, userID, keyOrID string) (*Worker, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+workerCols+` FROM project_workers
		WHERE user_id = $1 AND (id = $2 OR key = $2)`, userID, keyOrID)
	return scanWorker(row)
}

// GetWorkerBySession finds the worker row tied to an agent session id.
func (s *Store) GetWorkerBySession(ctx context.Context, userID, sessionID string) (*Worker, error) {
	if sessionID == "" {
		return nil, ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+workerCols+` FROM project_workers
		WHERE user_id = $1 AND session_id = $2
		ORDER BY created_at DESC LIMIT 1`, userID, sessionID)
	return scanWorker(row)
}

func (s *Store) ListWorkers(ctx context.Context, userID, projectID string, limit int) ([]*Worker, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+workerCols+` FROM project_workers
		WHERE user_id = $1 AND project_id = $2
		ORDER BY created_at DESC LIMIT $3`, userID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Worker{}
	for rows.Next() {
		w, err := scanWorker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// HasRunningWorker reports whether the user already has a running worker (serial slot).
func (s *Store) HasRunningWorker(ctx context.Context, userID string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM project_workers
		WHERE user_id = $1 AND status = $2`, userID, WorkerRunning).Scan(&n)
	return n > 0, err
}

// EnrichWorkerActivity fills MessageCount / LastMessageAt from agent_messages.
func (s *Store) EnrichWorkerActivity(ctx context.Context, workers []*Worker) error {
	ids := make([]string, 0, len(workers))
	seen := map[string]struct{}{}
	for _, w := range workers {
		if w == nil || w.SessionID == "" {
			continue
		}
		if _, ok := seen[w.SessionID]; ok {
			continue
		}
		seen[w.SessionID] = struct{}{}
		ids = append(ids, w.SessionID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT session_id, COUNT(*)::int, MAX(created_at)
		FROM agent_messages
		WHERE session_id = ANY($1)
		GROUP BY session_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type act struct {
		n  int
		at time.Time
	}
	by := map[string]act{}
	for rows.Next() {
		var sid string
		var n int
		var at time.Time
		if err := rows.Scan(&sid, &n, &at); err != nil {
			return err
		}
		by[sid] = act{n: n, at: at.UTC()}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, w := range workers {
		if w == nil || w.SessionID == "" {
			continue
		}
		if a, ok := by[w.SessionID]; ok {
			w.MessageCount = a.n
			t := a.at
			w.LastMessageAt = &t
		}
	}
	return nil
}

// HasActiveWorkerForIssue reports queued/running workers linked to the issue.
func (s *Store) HasActiveWorkerForIssue(ctx context.Context, userID, issueID string) (bool, error) {
	if strings.TrimSpace(issueID) == "" {
		return false, nil
	}
	var n int
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM project_workers
		WHERE user_id = $1 AND issue_id = $2 AND status IN ($3, $4)`,
		userID, issueID, WorkerQueued, WorkerRunning).Scan(&n)
	return n > 0, err
}

// ClaimNextQueued marks the oldest queued worker for the user as running, if none is running.
func (s *Store) ClaimNextQueued(ctx context.Context, userID string) (*Worker, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var running int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM project_workers
		WHERE user_id = $1 AND status = $2`, userID, WorkerRunning).Scan(&running); err != nil {
		return nil, err
	}
	if running > 0 {
		return nil, nil
	}

	row := tx.QueryRowContext(ctx, `
		SELECT `+workerCols+` FROM project_workers
		WHERE user_id = $1 AND status = $2
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, userID, WorkerQueued)
	w, err := scanWorker(row)
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		UPDATE project_workers SET status = $2, started_at = $3, updated_at = $3
		WHERE id = $1`, w.ID, WorkerRunning, now)
	if err != nil {
		// Another claim won the per-user running slot.
		if isUniqueViolation(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		if isUniqueViolation(err) {
			return nil, nil
		}
		return nil, err
	}
	w.Status = WorkerRunning
	w.StartedAt = &now
	w.UpdatedAt = now
	return w, nil
}

func (s *Store) FinishWorker(ctx context.Context, id, status, errMsg string) error {
	if status != WorkerSucceeded && status != WorkerFailed && status != WorkerCancelled {
		return fmtInvalidStatus(status)
	}
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE project_workers SET
			status = $2, error = $3, finished_at = $4, updated_at = $4
		WHERE id = $1 AND status = $5`,
		id, status, errMsg, now, WorkerRunning)
	return err
}

// FailOrphanRunning marks every running worker failed. Execute goroutines do not
// survive a process restart, so a leftover running row permanently blocks the
// per-user serial queue (one_running_per_user). Call on daemon boot.
func (s *Store) FailOrphanRunning(ctx context.Context, errMsg string) ([]*Worker, error) {
	if strings.TrimSpace(errMsg) == "" {
		errMsg = "interrupted by process restart"
	}
	return s.failRunningWhere(ctx, errMsg, 0)
}

// FailStaleRunning fails workers that have been running longer than maxAge
// (based on started_at, falling back to updated_at). Backup for hung execute
// goroutines that ignore the prompt context deadline.
func (s *Store) FailStaleRunning(ctx context.Context, maxAge time.Duration, errMsg string) ([]*Worker, error) {
	if maxAge <= 0 {
		maxAge = DefaultWorkerRunTimeout
	}
	if strings.TrimSpace(errMsg) == "" {
		errMsg = "execution timed out"
	}
	return s.failRunningWhere(ctx, errMsg, maxAge)
}

// ListUsersWithQueuedFreeSlot returns users that have queued workers and no
// running worker — Kick may have been missed after a finish/cancel race.
func (s *Store) ListUsersWithQueuedFreeSlot(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DISTINCT q.user_id
		FROM project_workers q
		WHERE q.status = $1
		  AND NOT EXISTS (
			SELECT 1 FROM project_workers r
			WHERE r.user_id = q.user_id AND r.status = $2
		  )
		ORDER BY q.user_id`, WorkerQueued, WorkerRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// failRunningWhere fails running workers. If olderThan > 0, only rows whose
// coalesce(started_at, updated_at) is older than now-olderThan are updated.
func (s *Store) failRunningWhere(ctx context.Context, errMsg string, olderThan time.Duration) ([]*Worker, error) {
	now := time.Now().UTC()
	var (
		rows *sql.Rows
		err  error
	)
	if olderThan <= 0 {
		rows, err = s.DB.QueryContext(ctx, `
			UPDATE project_workers SET
				status = $1, error = $2, finished_at = $3, updated_at = $3
			WHERE status = $4
			RETURNING `+workerCols,
			WorkerFailed, errMsg, now, WorkerRunning)
	} else {
		cutoff := now.Add(-olderThan)
		rows, err = s.DB.QueryContext(ctx, `
			UPDATE project_workers SET
				status = $1, error = $2, finished_at = $3, updated_at = $3
			WHERE status = $4
			  AND COALESCE(started_at, updated_at) <= $5
			RETURNING `+workerCols,
			WorkerFailed, errMsg, now, WorkerRunning, cutoff)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Worker{}
	for rows.Next() {
		w, err := scanWorker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// CancelWorker marks a queued or running worker cancelled. Returns false when
// the row was already terminal (or missing).
func (s *Store) CancelWorker(ctx context.Context, id string) (bool, error) {
	now := time.Now().UTC()
	res, err := s.DB.ExecContext(ctx, `
		UPDATE project_workers SET
			status = $2, error = $3, finished_at = $4, updated_at = $4
		WHERE id = $1 AND status IN ($5, $6)`,
		id, WorkerCancelled, "cancelled by user", now, WorkerQueued, WorkerRunning)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) SetWorkerSession(ctx context.Context, id, sessionID string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE project_workers SET session_id = $2, updated_at = $3 WHERE id = $1`,
		id, sessionID, time.Now().UTC())
	return err
}

// GetByCoordinatorSession finds the project whose coordinator chat is sessionID.
func (s *Store) GetByCoordinatorSession(ctx context.Context, userID, sessionID string) (*Project, error) {
	if sessionID == "" {
		return nil, ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+projectCols+` FROM projects
		WHERE user_id = $1 AND coordinator_session_id = $2 AND status = $3
		LIMIT 1`, userID, sessionID, StatusActive)
	return scanProject(row)
}

// GetByCoordinatorAssistant finds the active project for a coordinator assistant.
func (s *Store) GetByCoordinatorAssistant(ctx context.Context, userID, assistantID string) (*Project, error) {
	if assistantID == "" {
		return nil, ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+projectCols+` FROM projects
		WHERE user_id = $1 AND coordinator_assistant_id = $2 AND status = $3
		ORDER BY updated_at DESC LIMIT 1`, userID, assistantID, StatusActive)
	return scanProject(row)
}

func scanWorker(row rowScanner) (*Worker, error) {
	var w Worker
	err := row.Scan(
		&w.ID, &w.Key, &w.ProjectID, &w.UserID, &w.SessionID, &w.IssueID,
		&w.Title, &w.Brief, &w.Role, &w.Status, &w.Error, &w.ReviewJSON, &w.UserDecision,
		&w.CreatedAt, &w.UpdatedAt, &w.StartedAt, &w.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.Review = UnmarshalReview(w.ReviewJSON)
	return &w, nil
}

// SetWorkerReview stores structured reviewer output for a finished worker.
func (s *Store) SetWorkerReview(ctx context.Context, id string, review *ReviewResult) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE project_workers SET review_json = $2, updated_at = $3 WHERE id = $1`,
		id, MarshalReview(review), time.Now().UTC())
	return err
}

// SetWorkerUserDecision records Pass / Request changes (once).
func (s *Store) SetWorkerUserDecision(ctx context.Context, id, decision string) error {
	switch decision {
	case DecisionPassed, DecisionChangesRequested:
	default:
		return errors.New("decision must be passed or changes_requested")
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE project_workers SET user_decision = $2, updated_at = $3
		WHERE id = $1 AND user_decision = ''`,
		id, decision, time.Now().UTC())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("decision already recorded or worker not found")
	}
	return nil
}

func fmtInvalidStatus(status string) error {
	return errors.New("invalid worker status: " + status)
}
