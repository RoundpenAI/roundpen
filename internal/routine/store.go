package routine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Store persists routines and their runs.
type Store struct {
	DB *sql.DB
}

const routineCols = `id, key, user_id, COALESCE(assignee_assistant_id, ''), COALESCE(created_by_assistant_id, ''),
	created_by_session_id, COALESCE(issue_id, ''), title, brief, autonomy, hosts, cron, timezone,
	deliver_im, max_duration_sec, state, status, next_run_at, created_at, updated_at`

const runCols = `id, key, routine_id, user_id, COALESCE(assistant_id, ''), session_id, status,
	scheduled_at, started_at, finished_at, wait_started_at, summary, artifacts, error,
	deliver_error, assist_ticket_id, budget_left_sec, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

// Claim is a due routine the runner should start. NextRunAt on Routine is already
// the following fire; Run.ScheduledAt is the instant that was claimed.
type Claim struct {
	Routine Routine
	Run     Run
}

// Create inserts a routine and assigns RTN-n. Active routines get a next_run_at.
func (s *Store) Create(ctx context.Context, userID string, in CreateInput) (*Routine, error) {
	sched, err := ValidateCreate(&in)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hosts, err := json.Marshal(in.Hosts)
	if err != nil {
		return nil, err
	}
	if in.Hosts == nil {
		hosts = []byte(`[]`)
	}
	rt := &Routine{
		ID: uuid.NewString(), UserID: userID,
		AssigneeAssistantID: in.AssigneeAssistantID, CreatedByAssistantID: in.CreatedByAssistantID,
		CreatedBySessionID: in.CreatedBySessionID, IssueID: in.IssueID,
		Title: in.Title, Brief: in.Brief, Autonomy: in.Autonomy, Hosts: in.Hosts,
		Cron: in.Cron, Timezone: in.Timezone, DeliverIM: in.DeliverIM,
		MaxDurationSec: in.MaxDurationSec, State: json.RawMessage(`{}`),
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	next := sched.NextAfter(now)
	rt.NextRunAt = &next
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO routines (
			id, key, user_id, assignee_assistant_id, created_by_assistant_id, created_by_session_id,
			issue_id, title, brief, autonomy, hosts, cron, timezone, deliver_im, max_duration_sec,
			state, status, next_run_at, created_at, updated_at
		) VALUES (
			$1, 'RTN-' || nextval('routine_key_seq'), $2, NULLIF($3,''), NULLIF($4,''), $5,
			NULLIF($6,''), $7, $8, $9, $10, $11, $12, $13, $14,
			'{}', $15, $16, $17, $17
		) RETURNING key`,
		rt.ID, userID, rt.AssigneeAssistantID, rt.CreatedByAssistantID, rt.CreatedBySessionID,
		rt.IssueID, rt.Title, rt.Brief, rt.Autonomy, hosts, rt.Cron, rt.Timezone, rt.DeliverIM,
		rt.MaxDurationSec, rt.Status, next, now,
	).Scan(&rt.Key)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

// ListFilter selects a user's routines. Empty Status excludes archived.
type ListFilter struct {
	AssistantID string
	Status      string
	Limit       int
}

// List returns routines for a user, newest update first.
func (s *Store) List(ctx context.Context, userID string, f ListFilter) ([]Routine, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	q := `SELECT ` + routineCols + ` FROM routines WHERE user_id=$1`
	args := []any{userID}
	if f.Status != "" {
		args = append(args, f.Status)
		q += fmt.Sprintf(" AND status=$%d", len(args))
	} else {
		q += " AND status <> 'archived'"
	}
	if f.AssistantID != "" {
		args = append(args, f.AssistantID)
		q += fmt.Sprintf(" AND assignee_assistant_id=$%d", len(args))
	}
	args = append(args, f.Limit)
	q += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d", len(args))
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Routine
	for rows.Next() {
		rt, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rt)
	}
	return out, rows.Err()
}

// Get returns a routine and its most recent runs.
func (s *Store) Get(ctx context.Context, userID, keyOrID string) (*Routine, []Run, error) {
	rt, err := s.get(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, err
	}
	runs, err := s.ListRuns(ctx, userID, rt.Key, 10)
	if err != nil {
		return nil, nil, err
	}
	return rt, runs, nil
}

func (s *Store) get(ctx context.Context, userID, keyOrID string) (*Routine, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+routineCols+` FROM routines WHERE user_id=$1 AND (id=$2 OR key=$2)`,
		userID, keyOrID)
	rt, err := scanRoutine(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rt, err
}

// UpdateInput patches a routine. Nil fields are left alone.
type UpdateInput struct {
	Title               *string
	Brief               *string
	Autonomy            *string
	Hosts               []string
	HostsSet            bool
	Cron                *string
	Timezone            *string
	DeliverIM           *bool
	MaxDurationSec      *int
	Status              *string
	AssigneeAssistantID *string
	IssueID             *string
	IssueSet            bool
}

// Update applies a patch. Schedule and status changes recompute next_run_at.
func (s *Store) Update(ctx context.Context, userID, keyOrID string, in UpdateInput) (*Routine, error) {
	rt, err := s.get(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		rt.Title = *in.Title
	}
	if in.Brief != nil {
		rt.Brief = *in.Brief
	}
	if in.Autonomy != nil {
		rt.Autonomy = *in.Autonomy
	}
	if in.HostsSet {
		rt.Hosts = in.Hosts
	}
	if in.Cron != nil {
		rt.Cron = *in.Cron
	}
	if in.Timezone != nil {
		rt.Timezone = *in.Timezone
	}
	if in.DeliverIM != nil {
		rt.DeliverIM = *in.DeliverIM
	}
	if in.MaxDurationSec != nil {
		rt.MaxDurationSec = *in.MaxDurationSec
	}
	if in.Status != nil {
		rt.Status = *in.Status
	}
	if in.AssigneeAssistantID != nil {
		rt.AssigneeAssistantID = *in.AssigneeAssistantID
	}
	if in.IssueSet {
		if in.IssueID != nil {
			rt.IssueID = *in.IssueID
		} else {
			rt.IssueID = ""
		}
	}
	sched, err := ValidateCreate(&CreateInput{
		Title: rt.Title, Brief: rt.Brief, Autonomy: rt.Autonomy, Hosts: rt.Hosts,
		Cron: rt.Cron, Timezone: rt.Timezone, MaxDurationSec: rt.MaxDurationSec,
	})
	if err != nil {
		return nil, err
	}
	if err := validateStatus(rt.Status); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rt.UpdatedAt = now
	var next any
	if rt.Status == StatusActive {
		n := sched.NextAfter(now)
		rt.NextRunAt = &n
		next = n
	} else {
		rt.NextRunAt = nil
	}
	hosts, err := json.Marshal(rt.Hosts)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE routines SET
			title=$3, brief=$4, autonomy=$5, hosts=$6, cron=$7, timezone=$8,
			deliver_im=$9, max_duration_sec=$10, status=$11, assignee_assistant_id=NULLIF($12,''),
			issue_id=NULLIF($13,''), next_run_at=$14, updated_at=$15
		WHERE user_id=$1 AND id=$2`,
		userID, rt.ID, rt.Title, rt.Brief, rt.Autonomy, hosts, rt.Cron, rt.Timezone,
		rt.DeliverIM, rt.MaxDurationSec, rt.Status, rt.AssigneeAssistantID, rt.IssueID, next, now,
	)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

// UpdateState replaces the cursor blob. onlyAssignee, when set, must be the assignee.
func (s *Store) UpdateState(ctx context.Context, userID, keyOrID string, state json.RawMessage, onlyAssignee string) (*Routine, error) {
	raw, err := NormalizeState(state)
	if err != nil {
		return nil, err
	}
	rt, err := s.get(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	if onlyAssignee != "" && rt.AssigneeAssistantID != onlyAssignee {
		return nil, ErrForbidden
	}
	now := time.Now().UTC()
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE routines SET state=$3, updated_at=$4 WHERE user_id=$1 AND id=$2`,
		userID, rt.ID, []byte(raw), now); err != nil {
		return nil, err
	}
	rt.State = raw
	rt.UpdatedAt = now
	return rt, nil
}

// PauseByAssignee pauses every active routine owned by the assistant.
func (s *Store) PauseByAssignee(ctx context.Context, assistantID string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE routines SET status='paused', next_run_at=NULL, updated_at=$2
		WHERE assignee_assistant_id=$1 AND status='active'`,
		assistantID, time.Now().UTC())
	return err
}

// ListRuns returns runs newest first.
func (s *Store) ListRuns(ctx context.Context, userID, keyOrID string, limit int) ([]Run, error) {
	rt, err := s.get(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+runCols+` FROM routine_runs WHERE routine_id=$1 AND user_id=$2 ORDER BY created_at DESC LIMIT $3`,
		rt.ID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		rn, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rn)
	}
	return out, rows.Err()
}

// GetRunByKey loads a run by its key or id for this user.
func (s *Store) GetRunByKey(ctx context.Context, userID, runKey string) (*Run, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+runCols+` FROM routine_runs WHERE user_id=$1 AND (id=$2 OR key=$2)`, userID, runKey)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rn, err
}

// GetRun returns one run scoped to the user and its routine.
func (s *Store) GetRun(ctx context.Context, userID, routineKey, runKey string) (*Run, error) {
	rt, err := s.get(ctx, userID, routineKey)
	if err != nil {
		return nil, err
	}
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+runCols+` FROM routine_runs WHERE routine_id=$1 AND user_id=$2 AND (id=$3 OR key=$3)`,
		rt.ID, userID, runKey)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rn, err
}

// FinishRun marks a running run succeeded when the caller is its session.
func (s *Store) FinishRun(ctx context.Context, userID, runKey, sessionID, summary string, artifacts []Artifact) (*Run, error) {
	raw, err := json.Marshal(artifacts)
	if err != nil {
		return nil, err
	}
	if artifacts == nil {
		raw = []byte(`[]`)
	}
	now := time.Now().UTC()
	row := s.DB.QueryRowContext(ctx, `
		UPDATE routine_runs SET status=$4, summary=$5, artifacts=$6, finished_at=$7
		WHERE user_id=$1 AND (id=$2 OR key=$2) AND session_id=$3 AND status=$8
		RETURNING `+runCols,
		userID, runKey, sessionID, RunSucceeded, summary, raw, now, RunRunning)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return rn, err
}

// RequestConfirm parks a running run on an assist ticket and records leftover budget.
func (s *Store) RequestConfirm(ctx context.Context, userID, runKey, sessionID, ticketID string, budgetLeft int) (*Run, error) {
	now := time.Now().UTC()
	if budgetLeft < MinDurationSec {
		budgetLeft = MinDurationSec
	}
	row := s.DB.QueryRowContext(ctx, `
		UPDATE routine_runs SET status=$4, assist_ticket_id=$5, budget_left_sec=$6, wait_started_at=$7
		WHERE user_id=$1 AND (id=$2 OR key=$2) AND session_id=$3 AND status=$8
		RETURNING `+runCols,
		userID, runKey, sessionID, RunWaitingUser, ticketID, budgetLeft, now, RunRunning)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return rn, err
}

// Resume moves a waiting run back to running for the same session.
func (s *Store) Resume(ctx context.Context, runID string) (*Run, error) {
	row := s.DB.QueryRowContext(ctx, `
		UPDATE routine_runs SET status=$2, assist_ticket_id='', wait_started_at=NULL
		WHERE id=$1 AND status=$3
		RETURNING `+runCols,
		runID, RunRunning, RunWaitingUser)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rn, err
}

// CancelRun cancels a non-terminal run.
func (s *Store) CancelRun(ctx context.Context, userID, routineKey, runKey string) (*Run, error) {
	rt, err := s.get(ctx, userID, routineKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := s.DB.QueryRowContext(ctx, `
		UPDATE routine_runs SET status=$4, finished_at=$5
		WHERE routine_id=$1 AND user_id=$2 AND (id=$3 OR key=$3)
		  AND status IN ('queued', 'running', 'waiting_user')
		RETURNING `+runCols,
		rt.ID, userID, runKey, RunCancelled, now)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rn, err
}

// ClaimDue locks due routines, records skips, and returns fires to start.
// next_run_at moves to the next future instant in the same transaction.
func (s *Store) ClaimDue(ctx context.Context, limit int) ([]Claim, error) {
	if limit <= 0 {
		limit = 4
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM routines
		WHERE status = 'active'
		  AND assignee_assistant_id IS NOT NULL
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= $1
		ORDER BY next_run_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var claims []Claim
	for _, id := range ids {
		c, err := claimOne(ctx, tx, id, now)
		if err != nil {
			return nil, err
		}
		if c != nil {
			claims = append(claims, *c)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claims, nil
}

func claimOne(ctx context.Context, tx *sql.Tx, id string, now time.Time) (*Claim, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+routineCols+` FROM routines WHERE id=$1`, id)
	rt, err := scanRoutine(row)
	if err != nil {
		return nil, err
	}
	sched, err := ParseSchedule(rt.Cron, rt.Timezone)
	if err != nil {
		return nil, err
	}
	next := sched.NextAfter(now)
	var open bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM routine_runs
			WHERE routine_id=$1 AND status IN ('queued', 'running', 'waiting_user')
		)`, id).Scan(&open); err != nil {
		return nil, err
	}
	scheduled := now
	if rt.NextRunAt != nil {
		scheduled = *rt.NextRunAt
	}
	status := RunQueued
	if open {
		status = RunSkippedOverlap
	} else if ClassifyDue(scheduled, now, GraceFor(sched.Interval(scheduled))) == DueStale {
		status = RunSkippedStale
	}
	run, err := insertRun(ctx, tx, rt, status, scheduled, now)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE routines SET next_run_at=$2, updated_at=$3 WHERE id=$1`, id, next, now); err != nil {
		return nil, err
	}
	rt.NextRunAt = &next
	rt.UpdatedAt = now
	if status != RunQueued {
		return nil, nil
	}
	return &Claim{Routine: *rt, Run: *run}, nil
}

func insertRun(ctx context.Context, tx *sql.Tx, rt *Routine, status string, scheduled, now time.Time) (*Run, error) {
	finished := any(nil)
	if status == RunSkippedOverlap || status == RunSkippedStale {
		finished = now
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO routine_runs (
			id, key, routine_id, user_id, assistant_id, status, scheduled_at, finished_at, created_at
		) VALUES (
			$1, 'RUN-' || nextval('routine_run_key_seq'), $2, $3, NULLIF($4,''), $5, $6, $7, $8
		) RETURNING `+runCols,
		uuid.NewString(), rt.ID, rt.UserID, rt.AssigneeAssistantID, status, scheduled, finished, now)
	return scanRun(row)
}

// MarkRunning claims a queued run for execution. ok is false when another worker took it.
func (s *Store) MarkRunning(ctx context.Context, runID string) (*Run, bool, error) {
	now := time.Now().UTC()
	row := s.DB.QueryRowContext(ctx, `
		UPDATE routine_runs SET status=$2, started_at=$3
		WHERE id=$1 AND status=$4 AND started_at IS NULL
		RETURNING `+runCols,
		runID, RunRunning, now, RunQueued)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return rn, true, nil
}

// FailRun closes a non-terminal run as failed.
func (s *Store) FailRun(ctx context.Context, runID, summary, internal string) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE routine_runs SET status=$2, summary=$3, error=$4, finished_at=$5
		WHERE id=$1 AND status IN ('queued', 'running', 'waiting_user')`,
		runID, RunFailed, summary, internal, now)
	return err
}

// Pause updates one routine to paused. Used when the assignee can no longer run it.
func (s *Store) Pause(ctx context.Context, routineID string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE routines SET status='paused', next_run_at=NULL, updated_at=$2
		WHERE id=$1 AND status='active'`, routineID, time.Now().UTC())
	return err
}

// SetDeliverError records an IM failure without changing run status.
func (s *Store) SetDeliverError(ctx context.Context, runID, msg string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE routine_runs SET deliver_error=$2 WHERE id=$1`, runID, msg)
	return err
}

// AttachSession binds the execution session onto a run.
func (s *Store) AttachSession(ctx context.Context, runID, sessionID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE routine_runs SET session_id=$2 WHERE id=$1`, runID, sessionID)
	return err
}

// FindWaitingByTicket returns the waiting run parked on a ticket, if any.
func (s *Store) FindWaitingByTicket(ctx context.Context, ticketID string) (*Run, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+runCols+` FROM routine_runs WHERE assist_ticket_id=$1 AND status=$2`,
		ticketID, RunWaitingUser)
	rn, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rn, err
}

// ListStaleWaiting returns runs that have been waiting longer than the cutoff.
func (s *Store) ListStaleWaiting(ctx context.Context, olderThan time.Time) ([]Run, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+runCols+` FROM routine_runs WHERE status=$1 AND wait_started_at IS NOT NULL AND wait_started_at < $2`,
		RunWaitingUser, olderThan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		rn, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rn)
	}
	return out, rows.Err()
}

// ListOrphanQueued returns queued runs whose claim is old enough to retry after a crash.
func (s *Store) ListOrphanQueued(ctx context.Context, olderThan time.Time) ([]Run, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+runCols+` FROM routine_runs
		WHERE status=$1 AND started_at IS NULL AND created_at < $2`, RunQueued, olderThan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		rn, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rn)
	}
	return out, rows.Err()
}

// LastSuccessSummary is the summary of the newest succeeded run, or empty.
func (s *Store) LastSuccessSummary(ctx context.Context, routineID string) (string, error) {
	var summary string
	err := s.DB.QueryRowContext(ctx, `
		SELECT summary FROM routine_runs
		WHERE routine_id=$1 AND status=$2
		ORDER BY created_at DESC LIMIT 1`, routineID, RunSucceeded).Scan(&summary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return summary, err
}

func scanRoutine(row rowScanner) (*Routine, error) {
	var rt Routine
	var hosts, state []byte
	var next sql.NullTime
	if err := row.Scan(
		&rt.ID, &rt.Key, &rt.UserID, &rt.AssigneeAssistantID, &rt.CreatedByAssistantID,
		&rt.CreatedBySessionID, &rt.IssueID, &rt.Title, &rt.Brief, &rt.Autonomy, &hosts,
		&rt.Cron, &rt.Timezone, &rt.DeliverIM, &rt.MaxDurationSec, &state, &rt.Status,
		&next, &rt.CreatedAt, &rt.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(hosts) > 0 {
		_ = json.Unmarshal(hosts, &rt.Hosts)
	}
	if rt.Hosts == nil {
		rt.Hosts = []string{}
	}
	if len(state) == 0 {
		state = []byte(`{}`)
	}
	rt.State = state
	if next.Valid {
		t := next.Time.UTC()
		rt.NextRunAt = &t
	}
	return &rt, nil
}

func scanRun(row rowScanner) (*Run, error) {
	var rn Run
	var started, finished, waited sql.NullTime
	var artifacts []byte
	var budget sql.NullInt64
	if err := row.Scan(
		&rn.ID, &rn.Key, &rn.RoutineID, &rn.UserID, &rn.AssistantID, &rn.SessionID, &rn.Status,
		&rn.ScheduledAt, &started, &finished, &waited, &rn.Summary, &artifacts, &rn.Error,
		&rn.DeliverError, &rn.AssistTicketID, &budget, &rn.CreatedAt,
	); err != nil {
		return nil, err
	}
	if started.Valid {
		t := started.Time.UTC()
		rn.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time.UTC()
		rn.FinishedAt = &t
	}
	if waited.Valid {
		t := waited.Time.UTC()
		rn.WaitStartedAt = &t
	}
	if len(artifacts) == 0 {
		artifacts = []byte(`[]`)
	}
	rn.Artifacts = artifacts
	if budget.Valid {
		n := int(budget.Int64)
		rn.BudgetLeftSec = &n
	}
	rn.ScheduledAt = rn.ScheduledAt.UTC()
	return &rn, nil
}
