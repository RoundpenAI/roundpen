// Package agentsession persists Agent Web UI sessions.
package agentsession

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Session is a user-facing agent chat session.
type Session struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	Title       string    `json:"title"`
	ProviderID  string    `json:"providerId"`
	SandboxID   string    `json:"sandboxId"`
	AssistantID string    `json:"assistantId,omitempty"`
	Status      string    `json:"status"` // active | stopped
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Message is a persisted chat turn summary.
type Message struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Role      string          `json:"role"` // user | assistant | thought | tool | permission | event
	Content   string          `json:"content"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

const (
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleThought    = "thought"
	RoleTool       = "tool"
	RolePermission = "permission"
	RoleEvent      = "event"
)

// ToolMeta is stored in agent_messages.meta for role=tool rows.
type ToolMeta struct {
	Type   string `json:"type"`
	ToolID string `json:"toolId"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Input  any    `json:"input,omitempty"`
	Output any    `json:"output,omitempty"`
}

// MergeToolMeta keeps existing tool fields when a live update omits them.
func MergeToolMeta(prev, patch ToolMeta) ToolMeta {
	out := prev
	if patch.ToolID != "" {
		out.ToolID = patch.ToolID
	}
	if patch.Title != "" {
		out.Title = patch.Title
	}
	if patch.Status != "" {
		out.Status = patch.Status
	}
	if patch.Kind != "" {
		out.Kind = patch.Kind
	}
	if patch.Input != nil {
		out.Input = patch.Input
	}
	if patch.Output != nil {
		out.Output = patch.Output
	}
	out.Type = "tool_call"
	return out
}

// PermissionMeta is stored in agent_messages.meta for role=permission rows.
type PermissionMeta struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
	Title     string `json:"title,omitempty"`
	OptionID  string `json:"optionId,omitempty"`
	Outcome   string `json:"outcome,omitempty"` // requested | selected | cancelled | auto
	Options   any    `json:"options,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
}

// Store persists sessions and messages.
type Store struct {
	DB *sql.DB
}

// Create inserts a new session. assistantID may be empty for legacy callers.
func (s *Store) Create(ctx context.Context, userID, title, providerID, sandboxID, assistantID string) (*Session, error) {
	now := time.Now().UTC()
	sess := &Session{
		ID:          uuid.NewString(),
		UserID:      userID,
		Title:       title,
		ProviderID:  providerID,
		SandboxID:   sandboxID,
		AssistantID: assistantID,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO agent_sessions (id, user_id, title, provider_id, sandbox_id, assistant_id, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9)`,
		sess.ID, sess.UserID, sess.Title, sess.ProviderID, sess.SandboxID, sess.AssistantID, sess.Status, sess.CreatedAt, sess.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// UpdateSandbox sets sandbox id after provision (if created with placeholder).
func (s *Store) UpdateSandbox(ctx context.Context, id, sandboxID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE agent_sessions SET sandbox_id=$2, updated_at=$3 WHERE id=$1`, id, sandboxID, time.Now().UTC())
	return err
}

func scanSession(row interface{ Scan(dest ...any) error }) (*Session, error) {
	var sess Session
	var assistantID sql.NullString
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.Title, &sess.ProviderID, &sess.SandboxID, &assistantID, &sess.Status, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	if assistantID.Valid {
		sess.AssistantID = assistantID.String
	}
	return &sess, nil
}

const sessionCols = `id, user_id, title, provider_id, sandbox_id, assistant_id, status, created_at, updated_at`

// Get returns a session by id.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+sessionCols+`
		FROM agent_sessions WHERE id=$1`, id)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sess, nil
}

// ListByUser returns sessions for a user (newest first).
func (s *Store) ListByUser(ctx context.Context, userID string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+sessionCols+`
		FROM agent_sessions
		WHERE user_id=$1 AND status <> 'stopped'
		ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// ListByAssistant returns active sessions for an assistant (newest first).
func (s *Store) ListByAssistant(ctx context.Context, userID, assistantID string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+sessionCols+`
		FROM agent_sessions
		WHERE user_id=$1 AND assistant_id=$2 AND status <> 'stopped'
		ORDER BY updated_at DESC LIMIT $3`, userID, assistantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// MarkStopped marks a session stopped.
func (s *Store) MarkStopped(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE agent_sessions SET status='stopped', updated_at=$2 WHERE id=$1`, id, time.Now().UTC())
	return err
}

// Delete removes a session and cascaded messages.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM agent_sessions WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddMessage appends a message.
func (s *Store) AddMessage(ctx context.Context, sessionID, role, content string, meta any) (*Message, error) {
	var raw json.RawMessage
	if meta != nil {
		b, err := json.Marshal(meta)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	msg := &Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Meta:      raw,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO agent_messages (id, session_id, role, content, meta, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		msg.ID, msg.SessionID, msg.Role, msg.Content, nullJSON(msg.Meta), msg.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE agent_sessions SET updated_at=$2 WHERE id=$1`, sessionID, msg.CreatedAt)
	return msg, nil
}

func toolContent(m ToolMeta, fallback string) string {
	if strings.TrimSpace(m.Title) != "" {
		return m.Title
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	if strings.TrimSpace(m.ToolID) != "" {
		return m.ToolID
	}
	return "tool"
}

// UpsertToolMessage inserts a tool-call row or merges a later update onto the same toolId.
func (s *Store) UpsertToolMessage(ctx context.Context, sessionID string, patch ToolMeta) (*Message, error) {
	patch.Type = "tool_call"
	toolID := strings.TrimSpace(patch.ToolID)
	if toolID == "" {
		return s.AddMessage(ctx, sessionID, "tool", toolContent(patch, ""), patch)
	}

	row := s.DB.QueryRowContext(ctx, `
		SELECT id, content, meta, created_at FROM agent_messages
		WHERE session_id=$1 AND role='tool' AND meta->>'toolId'=$2
		ORDER BY created_at ASC LIMIT 1`, sessionID, toolID)
	var (
		id, content string
		meta        sql.NullString
		created     time.Time
	)
	err := row.Scan(&id, &content, &meta, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return s.AddMessage(ctx, sessionID, "tool", toolContent(patch, ""), patch)
	}
	if err != nil {
		return nil, err
	}

	var prev ToolMeta
	if meta.Valid && meta.String != "" {
		_ = json.Unmarshal([]byte(meta.String), &prev)
	}
	merged := MergeToolMeta(prev, patch)
	title := toolContent(merged, content)
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err := s.DB.ExecContext(ctx, `
		UPDATE agent_messages SET content=$2, meta=$3 WHERE id=$1`,
		id, title, raw,
	); err != nil {
		return nil, err
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE agent_sessions SET updated_at=$2 WHERE id=$1`, sessionID, now)
	return &Message{
		ID:        id,
		SessionID: sessionID,
		Role:      "tool",
		Content:   title,
		Meta:      raw,
		CreatedAt: created,
	}, nil
}

// ListMessages returns messages for a session.
func (s *Store) ListMessages(ctx context.Context, sessionID string, limit int) ([]*Message, error) {
	if limit <= 0 {
		limit = 2000
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, session_id, role, content, meta, created_at
		FROM agent_messages WHERE session_id=$1 ORDER BY created_at ASC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		var msg Message
		var meta sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &meta, &msg.CreatedAt); err != nil {
			return nil, err
		}
		if meta.Valid {
			msg.Meta = json.RawMessage(meta.String)
		}
		out = append(out, &msg)
	}
	return out, rows.Err()
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

// ErrNotFound is returned when a session is missing.
var ErrNotFound = fmt.Errorf("agent session not found")
