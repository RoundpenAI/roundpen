package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/workspace"
	"github.com/google/uuid"
)

// ErrNotFound is returned when a project key does not resolve for the user.
var ErrNotFound = errors.New("project not found")

// ErrSlugTaken is returned when the user already has a project with that slug.
var ErrSlugTaken = errors.New("project slug already exists")

// Store persists projects.
type Store struct {
	DB *sql.DB
}

const projectCols = `id, key, user_id, name, icon, slug, repo_path, repo_url,
	COALESCE(coordinator_assistant_id, ''), coordinator_session_id,
	status, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

// Create inserts a project row. The caller creates the coordinator assistant
// first and passes its id.
func (s *Store) Create(ctx context.Context, userID string, in CreateInput, coordinatorID string) (*Project, error) {
	if err := ValidateName(in.Name); err != nil {
		return nil, err
	}
	if err := ValidateRepoURL(in.RepoURL); err != nil {
		return nil, err
	}
	slug, err := ResolveSlug(in.Slug, in.Name)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	p := &Project{
		ID:                     uuid.NewString(),
		UserID:                 userID,
		Name:                   strings.TrimSpace(in.Name),
		Icon:                   strings.TrimSpace(in.Icon),
		Slug:                   slug,
		RepoPath:               workspace.ProjectRelPath(slug),
		RepoURL:                strings.TrimSpace(in.RepoURL),
		CoordinatorAssistantID: coordinatorID,
		Status:                 StatusActive,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO projects (
			id, key, user_id, name, icon, slug, repo_path, repo_url,
			coordinator_assistant_id, coordinator_session_id, status, created_at, updated_at
		) VALUES (
			$1, 'PRJ-' || nextval('project_key_seq'), $2, $3, $4, $5, $6, $7,
			NULLIF($8, ''), '', $9, $10, $10
		)
		RETURNING key`,
		p.ID, userID, p.Name, p.Icon, p.Slug, p.RepoPath, p.RepoURL,
		coordinatorID, p.Status, now,
	).Scan(&p.Key)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, err
	}
	return p, nil
}

func (s *Store) Get(ctx context.Context, userID, keyOrID string) (*Project, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+projectCols+` FROM projects
		WHERE user_id = $1 AND (id = $2 OR key = $2)`,
		userID, keyOrID)
	return scanProject(row)
}

func (s *Store) List(ctx context.Context, userID string, f ListFilter) ([]*Project, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `
		SELECT ` + projectCols + ` FROM projects
		WHERE user_id = $1`
	args := []any{userID}
	if f.Status != "" {
		if err := ValidateStatus(f.Status); err != nil {
			return nil, err
		}
		args = append(args, f.Status)
		q += fmt.Sprintf(` AND status = $%d`, len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d`, len(args))

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []*Project{}
	}
	return out, rows.Err()
}

func (s *Store) Update(ctx context.Context, userID, keyOrID string, in UpdateInput) (*Project, error) {
	cur, err := s.Get(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		if err := ValidateName(*in.Name); err != nil {
			return nil, err
		}
		cur.Name = strings.TrimSpace(*in.Name)
	}
	if in.Icon != nil {
		cur.Icon = strings.TrimSpace(*in.Icon)
	}
	if in.RepoURL != nil {
		if err := ValidateRepoURL(*in.RepoURL); err != nil {
			return nil, err
		}
		cur.RepoURL = strings.TrimSpace(*in.RepoURL)
	}
	if in.Status != nil {
		if err := ValidateStatus(*in.Status); err != nil {
			return nil, err
		}
		cur.Status = *in.Status
	}
	now := time.Now().UTC()
	cur.UpdatedAt = now
	_, err = s.DB.ExecContext(ctx, `
		UPDATE projects SET
			name = $3, icon = $4, repo_url = $5, status = $6, updated_at = $7
		WHERE user_id = $1 AND id = $2`,
		userID, cur.ID, cur.Name, cur.Icon, cur.RepoURL, cur.Status, now,
	)
	if err != nil {
		return nil, err
	}
	return cur, nil
}

// SetCoordinatorSession records the latest coordinator chat session id.
func (s *Store) SetCoordinatorSession(ctx context.Context, userID, keyOrID, sessionID string) (*Project, error) {
	p, err := s.Get(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.DB.ExecContext(ctx, `
		UPDATE projects SET coordinator_session_id = $3, updated_at = $4
		WHERE user_id = $1 AND id = $2`,
		userID, p.ID, sessionID, now,
	)
	if err != nil {
		return nil, err
	}
	p.CoordinatorSessionID = sessionID
	p.UpdatedAt = now
	return p, nil
}

func scanProject(row rowScanner) (*Project, error) {
	var p Project
	err := row.Scan(
		&p.ID, &p.Key, &p.UserID, &p.Name, &p.Icon, &p.Slug, &p.RepoPath, &p.RepoURL,
		&p.CoordinatorAssistantID, &p.CoordinatorSessionID,
		&p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key")
}
