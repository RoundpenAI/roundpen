package issue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a key does not resolve for the requesting user.
// Every read and write is scoped by user_id in SQL, not only by callers.
var ErrNotFound = errors.New("issue not found")

// Store persists issues, their versioned documents and their tasks.
type Store struct {
	DB *sql.DB
}

// issueCols, taskCols and docCols keep select lists and the write paths'
// RETURNING clauses in one place, in scan order.
const issueCols = `id, key, user_id, COALESCE(assistant_id, ''), session_id, title, summary,
	status, origin, created_at, updated_at, closed_at`

const taskCols = `id, key, issue_id, COALESCE(plan_doc_id, ''), position, title, detail,
	status, session_id, COALESCE(assistant_id, ''), created_at, updated_at, done_at`

// docCols omits content_md: list queries return indexes, not bodies.
const docCols = `id, key, issue_id, COALESCE(task_id, ''), kind, version, status, title,
	author_type, COALESCE(assistant_id, ''), session_id, created_at`

// rowScanner is satisfied by *sql.Row and *sql.Rows, so one scan helper serves
// single-row and list queries.
type rowScanner interface {
	Scan(dest ...any) error
}

// queryer is satisfied by *sql.DB and *sql.Tx, so the task select runs both
// standalone and inside a write transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type CreateInput struct {
	Title       string
	Summary     string
	AssistantID string
	SessionID   string
	Origin      string
}

// Create inserts an issue, assigning its short key from issue_key_seq in the same
// statement so concurrent creates cannot collide.
func (s *Store) Create(ctx context.Context, userID string, in CreateInput) (*Issue, error) {
	if err := ValidateTitle(in.Title); err != nil {
		return nil, err
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginChat
	}
	now := time.Now().UTC()
	it := &Issue{
		ID: uuid.NewString(), UserID: userID,
		AssistantID: in.AssistantID, SessionID: in.SessionID,
		Title: strings.TrimSpace(in.Title), Summary: strings.TrimSpace(in.Summary),
		Status: StatusDrafting, Origin: origin, CreatedAt: now, UpdatedAt: now,
	}
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO issues (id, key, user_id, assistant_id, session_id, title, summary, status, origin, created_at, updated_at)
		VALUES ($1, 'ISS-' || nextval('issue_key_seq'), $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, $9)
		RETURNING key`,
		it.ID, userID, it.AssistantID, it.SessionID, it.Title, it.Summary, it.Status, it.Origin, now,
	).Scan(&it.Key)
	if err != nil {
		return nil, err
	}
	return it, nil
}

// SessionAssistant resolves a session id to the assistant bound to it, scoped to
// the caller. ErrNotFound means the session does not exist or is not the
// caller's, so ids belonging to others cannot be attributed to this user.
// assistant_id is nullable (sessions predate assistants); NULL reads as "".
func (s *Store) SessionAssistant(ctx context.Context, userID, sessionID string) (string, error) {
	var assistantID string
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(assistant_id, '') FROM agent_sessions WHERE id = $1 AND user_id = $2`,
		sessionID, userID).Scan(&assistantID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return assistantID, err
}

// Get returns an issue with its document index and task list.
func (s *Store) Get(ctx context.Context, userID, keyOrID string) (*Issue, []Doc, []Task, error) {
	it, err := s.getIssue(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, nil, err
	}
	docs, err := s.ListDocs(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, nil, err
	}
	tasks, err := s.ListTasks(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, nil, err
	}
	return it, docs, tasks, nil
}

func (s *Store) getIssue(ctx context.Context, userID, keyOrID string) (*Issue, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+issueCols+` FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1`,
		userID, keyOrID)
	return scanIssue(row)
}

// resolveIssueID locks the issue row and returns its id. Used inside write
// transactions so per-issue version numbers and status transitions serialize.
func resolveIssueID(ctx context.Context, tx *sql.Tx, userID, keyOrID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1 FOR UPDATE`,
		userID, keyOrID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// lookupIssueID resolves keyOrID without locking; reads use this, writes use
// resolveIssueID inside their transaction.
func (s *Store) lookupIssueID(ctx context.Context, userID, keyOrID string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1`,
		userID, keyOrID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

type ListFilter struct {
	Status string
	Limit  int
}

func (s *Store) List(ctx context.Context, userID string, f ListFilter) ([]Issue, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+issueCols+` FROM issues
		WHERE user_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY updated_at DESC LIMIT $3`, userID, f.Status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		it, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

type UpdateInput struct {
	Status  *string
	Title   *string
	Summary *string
}

// Update applies an explicit change. The user (or console) has final say, so no
// status machine is applied here; closed_at still tracks done/cancelled.
func (s *Store) Update(ctx context.Context, userID, keyOrID string, in UpdateInput) (*Issue, error) {
	if in.Status != nil {
		if err := ValidateIssueStatus(*in.Status); err != nil {
			return nil, err
		}
	}
	if in.Title != nil {
		if err := ValidateTitle(*in.Title); err != nil {
			return nil, err
		}
	}
	row := s.DB.QueryRowContext(ctx, `
		UPDATE issues SET
			status     = COALESCE($3, status),
			title      = COALESCE($4, title),
			summary    = COALESCE($5, summary),
			updated_at = now(),
			closed_at  = CASE
				WHEN COALESCE($3, status) IN ('done', 'cancelled') THEN COALESCE(closed_at, now())
				ELSE NULL END
		WHERE (id = $2 OR key = $2) AND user_id = $1
		RETURNING `+issueCols,
		userID, keyOrID, in.Status, in.Title, in.Summary)
	return scanIssue(row)
}

// ListDocs returns the issue's document index, newest version first. Bodies are
// deliberately omitted — read them with GetDoc. Resolving the issue first, like
// ListTasks, keeps an unknown key a 404 rather than an empty list.
func (s *Store) ListDocs(ctx context.Context, userID, keyOrID string) ([]Doc, error) {
	issueID, err := s.lookupIssueID(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+docCols+` FROM issue_docs
		WHERE issue_id = $1
		ORDER BY kind, version DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDoc returns one document, body included. docKey accepts a short key
// (DOC-12), a uuid, "latest" or "" (both mean the current version); version > 0
// pins an exact version instead. version <= 0 prefers the row with
// status='current' and falls back to the highest version. The doc is resolved
// through its issue, so another user's documents read as ErrNotFound.
func (s *Store) GetDoc(ctx context.Context, userID, keyOrID, docKey string, version int) (*Doc, error) {
	q := `SELECT ` + docCols + `, content_md FROM issue_docs
		WHERE issue_id = (SELECT id FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1)`
	args := []any{userID, keyOrID}
	if docKey != "" && docKey != "latest" {
		args = append(args, docKey)
		q += fmt.Sprintf(` AND (id = $%d OR key = $%d)`, len(args), len(args))
	}
	if version > 0 {
		args = append(args, version)
		q += fmt.Sprintf(` AND version = $%d`, len(args))
	}
	// One query for both cases: the current row wins, otherwise the newest.
	q += ` ORDER BY (status = 'current') DESC, version DESC LIMIT 1`
	return scanDocWithBody(s.DB.QueryRowContext(ctx, q, args...))
}

type DocInput struct {
	Kind        string
	Title       string
	ContentMD   string
	Status      string // 缺省 current
	TaskKey     string
	AuthorType  string // 缺省 assistant
	AssistantID string
	SessionID   string
}

// WriteDoc appends a new version of the issue's spec or plan. Writing a current
// version supersedes the previous one and advances the issue status, all in one
// transaction so readers never see two current versions. A draft neither
// supersedes nor advances.
func (s *Store) WriteDoc(ctx context.Context, userID, keyOrID string, in DocInput) (*Doc, error) {
	if err := ValidateKind(in.Kind); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ContentMD) == "" {
		return nil, fmt.Errorf("contentMd is required")
	}
	status := in.Status
	if status == "" {
		status = DocCurrent
	}
	if err := ValidateDocStatus(status); err != nil {
		return nil, err
	}
	authorType := in.AuthorType
	if authorType == "" {
		authorType = "assistant"
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	issueID, err := resolveIssueID(ctx, tx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	var taskID string
	if in.TaskKey != "" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM tasks WHERE (id = $2 OR key = $2) AND user_id = $1 AND issue_id = $3`,
			userID, in.TaskKey, issueID).Scan(&taskID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	var version int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM issue_docs WHERE issue_id = $1 AND kind = $2`,
		issueID, in.Kind).Scan(&version); err != nil {
		return nil, err
	}
	if status == DocCurrent {
		if _, err := tx.ExecContext(ctx, `
			UPDATE issue_docs SET status = 'superseded'
			WHERE issue_id = $1 AND kind = $2 AND status = 'current'`, issueID, in.Kind); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	doc := &Doc{
		ID: uuid.NewString(), IssueID: issueID, TaskID: taskID, Kind: in.Kind,
		Version: version, Status: status, Title: strings.TrimSpace(in.Title),
		ContentMD: in.ContentMD, AuthorType: authorType,
		AssistantID: in.AssistantID, SessionID: in.SessionID, CreatedAt: now,
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO issue_docs (id, key, issue_id, task_id, kind, version, status, title,
			content_md, author_type, assistant_id, session_id, created_at)
		VALUES ($1, 'DOC-' || nextval('issue_doc_key_seq'), $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9,
			NULLIF($10, ''), $11, $12)
		RETURNING key`,
		doc.ID, issueID, taskID, in.Kind, version, status, doc.Title, in.ContentMD,
		authorType, in.AssistantID, in.SessionID, now).Scan(&doc.Key); err != nil {
		return nil, err
	}
	if status == DocCurrent {
		ev := EventSpecCurrent
		if in.Kind == DocPlan {
			ev = EventPlanCurrent
		}
		if err := advanceIssueStatus(ctx, tx, issueID, ev); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return doc, nil
}

// lockIssueStatus reads the issue status under a row lock. Every transition locks
// the issue first: the rules depend on other rows (task list, docs), and two
// concurrent writes must not each decide from a stale status.
func lockIssueStatus(ctx context.Context, tx *sql.Tx, issueID string) (string, error) {
	var cur string
	err := tx.QueryRowContext(ctx, `SELECT status FROM issues WHERE id = $1 FOR UPDATE`, issueID).Scan(&cur)
	return cur, err
}

// advanceIssueStatus locks, decides and writes. Callers that already hold the lock
// (WriteDoc and CreateTask go through resolveIssueID) simply re-acquire it for free.
func advanceIssueStatus(ctx context.Context, tx *sql.Tx, issueID string, ev Event) error {
	cur, err := lockIssueStatus(ctx, tx, issueID)
	if err != nil {
		return err
	}
	return applyIssueStatus(ctx, tx, issueID, cur, ev)
}

// applyIssueStatus writes the forward-only rule's result, keeping closed_at in step.
func applyIssueStatus(ctx context.Context, tx *sql.Tx, issueID, cur string, ev Event) error {
	next := Advance(cur, ev)
	if next == cur {
		return nil
	}
	if next == StatusDone || next == StatusCancelled {
		_, err := tx.ExecContext(ctx, `
			UPDATE issues SET status = $2, updated_at = now(), closed_at = COALESCE(closed_at, now())
			WHERE id = $1`, issueID, next)
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE issues SET status = $2, updated_at = now(), closed_at = NULL WHERE id = $1`, issueID, next)
	return err
}

type TaskInput struct {
	Title       string
	Detail      string
	Position    int
	Status      string // 缺省 todo
	PlanDocKey  string
	SessionID   string
	AssistantID string
}

// CreateTask adds a task to the issue. A task created straight into a live or
// terminal status counts as work started or finished, so the issue advances.
func (s *Store) CreateTask(ctx context.Context, userID, keyOrID string, in TaskInput) (*Task, error) {
	if err := ValidateTitle(in.Title); err != nil {
		return nil, err
	}
	status := in.Status
	if status == "" {
		status = TaskTodo
	}
	if err := ValidateTaskStatus(status); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	issueID, err := resolveIssueID(ctx, tx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	var planDocID string
	if in.PlanDocKey != "" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM issue_docs
			WHERE (id = $2 OR key = $2) AND issue_id = $3`,
			userID, in.PlanDocKey, issueID).Scan(&planDocID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	position := in.Position
	if position <= 0 { // 未指定时排到末尾
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE issue_id = $1`, issueID).Scan(&position); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	task := &Task{
		ID: uuid.NewString(), IssueID: issueID, PlanDocID: planDocID, Position: position,
		Title: strings.TrimSpace(in.Title), Detail: strings.TrimSpace(in.Detail), Status: status,
		SessionID: in.SessionID, AssistantID: in.AssistantID, CreatedAt: now, UpdatedAt: now,
	}
	if status == TaskDone {
		task.DoneAt = &now
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO tasks (id, key, issue_id, user_id, plan_doc_id, position, title, detail, status,
			session_id, assistant_id, created_at, updated_at, done_at)
		VALUES ($1, 'TSK-' || nextval('task_key_seq'), $2, $3, NULLIF($4, ''), $5, $6, $7, $8,
			$9, NULLIF($10, ''), $11, $11, $12)
		RETURNING key`,
		task.ID, issueID, userID, planDocID, position, task.Title, task.Detail, status,
		in.SessionID, in.AssistantID, now, task.DoneAt).Scan(&task.Key); err != nil {
		return nil, err
	}
	if status != TaskTodo { // 直接建成 in_progress/done 也算开工 / 完成
		if err := advanceForTaskChange(ctx, tx, issueID, status); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

type TaskUpdateInput struct {
	Status    *string
	Title     *string
	Detail    *string
	SessionID *string
}

// UpdateTask applies a partial update. A status change also drives the issue
// status in the same transaction; done_at follows the task status.
func (s *Store) UpdateTask(ctx context.Context, userID, keyOrID string, in TaskUpdateInput) (*Task, error) {
	if in.Status != nil {
		if err := ValidateTaskStatus(*in.Status); err != nil {
			return nil, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	task, err := scanTask(tx.QueryRowContext(ctx, `
		UPDATE tasks SET
			status     = COALESCE($3, status),
			title      = COALESCE($4, title),
			detail     = COALESCE($5, detail),
			session_id = COALESCE($6, session_id),
			updated_at = now(),
			done_at    = CASE
				WHEN COALESCE($3, status) = 'done' THEN COALESCE(done_at, now())
				WHEN COALESCE($3, status) = 'cancelled' THEN done_at
				ELSE NULL END
		WHERE (id = $2 OR key = $2) AND user_id = $1
		RETURNING `+taskCols,
		userID, keyOrID, in.Status, in.Title, in.Detail, in.SessionID))
	if err != nil {
		return nil, err
	}
	if in.Status != nil {
		if err := advanceForTaskChange(ctx, tx, task.IssueID, *in.Status); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

// advanceForTaskChange maps a task status write to an issue event: entering
// in_progress (or being reopened) means work is live; a terminal status means
// the completion rule may now hold.
//
// The issue lock is taken before the task list is read. Two tasks of the same
// issue completed concurrently would otherwise each see the other as still open
// and neither would fire all_tasks_done, leaving a finished issue in_progress.
func advanceForTaskChange(ctx context.Context, tx *sql.Tx, issueID, taskStatus string) error {
	cur, err := lockIssueStatus(ctx, tx, issueID)
	if err != nil {
		return err
	}
	switch taskStatus {
	case TaskInProgress, TaskTodo, TaskBlocked:
		return applyIssueStatus(ctx, tx, issueID, cur, EventTaskOpen)
	case TaskDone, TaskCancelled:
		all, err := listTasks(ctx, tx, issueID)
		if err != nil {
			return err
		}
		if AllTasksDone(all) {
			return applyIssueStatus(ctx, tx, issueID, cur, EventAllTasksDone)
		}
	}
	return nil
}

// ListTasks returns an issue's tasks in position order.
func (s *Store) ListTasks(ctx context.Context, userID, keyOrID string) ([]Task, error) {
	issueID, err := s.lookupIssueID(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	return listTasks(ctx, s.DB, issueID)
}

// listTasks backs both ListTasks and the write path's completion check, so q is
// either the store's DB or the write transaction's Tx.
func listTasks(ctx context.Context, q queryer, issueID string) ([]Task, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT `+taskCols+` FROM tasks WHERE issue_id = $1
		ORDER BY position, created_at`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

// scanIssue reads an issueCols row. closed_at is nullable and lands on the
// *time.Time field, which scans as nil for NULL.
func scanIssue(row rowScanner) (*Issue, error) {
	var it Issue
	err := row.Scan(&it.ID, &it.Key, &it.UserID, &it.AssistantID, &it.SessionID,
		&it.Title, &it.Summary, &it.Status, &it.Origin,
		&it.CreatedAt, &it.UpdatedAt, &it.ClosedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// scanTask reads a taskCols row.
func scanTask(row rowScanner) (*Task, error) {
	var task Task
	err := row.Scan(&task.ID, &task.Key, &task.IssueID, &task.PlanDocID, &task.Position,
		&task.Title, &task.Detail, &task.Status, &task.SessionID, &task.AssistantID,
		&task.CreatedAt, &task.UpdatedAt, &task.DoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// scanDoc reads a docCols row — the index shape, body excluded.
func scanDoc(row rowScanner) (*Doc, error) {
	return scanDocInto(row, nil)
}

// scanDocWithBody reads docCols with the content_md column GetDoc appends.
func scanDocWithBody(row rowScanner) (*Doc, error) {
	var body string
	doc, err := scanDocInto(row, &body)
	if err != nil {
		return nil, err
	}
	doc.ContentMD = body
	return doc, nil
}

func scanDocInto(row rowScanner, body *string) (*Doc, error) {
	var doc Doc
	dest := []any{&doc.ID, &doc.Key, &doc.IssueID, &doc.TaskID, &doc.Kind, &doc.Version,
		&doc.Status, &doc.Title, &doc.AuthorType, &doc.AssistantID, &doc.SessionID, &doc.CreatedAt}
	if body != nil {
		dest = append(dest, body)
	}
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &doc, nil
}
