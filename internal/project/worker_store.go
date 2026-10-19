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
