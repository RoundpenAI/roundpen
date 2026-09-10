// Package storage holds PostgreSQL connectivity and sandbox persistence.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// ErrNotFound is returned when a sandbox row does not exist.
// Deprecated: prefer sandbox.ErrNotFound (same sentinel).
var ErrNotFound = sandbox.ErrNotFound

// DB wraps database/sql for Roundpen.
type DB struct {
	SQL *sql.DB
}

// OpenPostgres opens a pgx-backed database/sql pool.
func OpenPostgres(ctx context.Context, databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	sqldb, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(10)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(time.Hour)
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{SQL: sqldb}, nil
}

// Close closes the pool.
func (db *DB) Close() error {
	if db == nil || db.SQL == nil {
		return nil
	}
	return db.SQL.Close()
}

// Migrate applies the bundled schema file.
func (db *DB) Migrate(ctx context.Context, schemaPath string) error {
	body, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	_, err = db.SQL.ExecContext(ctx, string(body))
	return err
}

// SandboxStore persists sandbox records.
type SandboxStore struct {
	db *DB
}

// NewSandboxStore returns a store.
func NewSandboxStore(db *DB) *SandboxStore {
	return &SandboxStore{db: db}
}

const sandboxCols = `id, container_id, image, status, workspace_id, workspace_path,
	metadata, ttl_seconds, expires_at, last_active_at, created_at, updated_at, name, category, is_default,
	cpu_count, memory_mb, disk_size_mb, template_build_id, COALESCE(owner, '')`

// Insert creates a sandbox row.
func (s *SandboxStore) Insert(ctx context.Context, sb *sandbox.Sandbox) error {
	meta, err := json.Marshal(sb.Metadata)
	if err != nil {
		return err
	}
	if meta == nil {
		meta = []byte("{}")
	}
	_, err = s.db.SQL.ExecContext(ctx, `
		INSERT INTO sandboxes (
			id, container_id, image, status, workspace_id, workspace_path,
			metadata, ttl_seconds, expires_at, last_active_at, created_at, updated_at,
			name, category, is_default, cpu_count, memory_mb, disk_size_mb, template_build_id, owner
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		sb.ID, sb.ContainerID, sb.Image, string(sb.Status), sb.WorkspaceID, sb.WorkspacePath,
		meta, sb.TTLSeconds, nullTime(sb.ExpiresAt), sb.LastActiveAt, sb.CreatedAt, sb.UpdatedAt,
		sb.Name, sb.Category, sb.IsDefault, sb.CPUCount, sb.MemoryMB, sb.DiskSizeMB, sb.TemplateBuild, sb.Owner,
	)
	return mapUniqueViolation(err)
}

// Update writes mutable fields.
func (s *SandboxStore) Update(ctx context.Context, sb *sandbox.Sandbox) error {
	meta, err := json.Marshal(sb.Metadata)
	if err != nil {
		return err
	}
	if meta == nil {
		meta = []byte("{}")
	}
	res, err := s.db.SQL.ExecContext(ctx, `
		UPDATE sandboxes SET
			container_id=$2, image=$3, status=$4, workspace_id=$5, workspace_path=$6,
			metadata=$7, ttl_seconds=$8, expires_at=$9, last_active_at=$10, updated_at=$11,
			name=$12, category=$13, is_default=$14, cpu_count=$15, memory_mb=$16,
			disk_size_mb=$17, template_build_id=$18, owner=$19
		WHERE id=$1 AND deleted_at IS NULL`,
		sb.ID, sb.ContainerID, sb.Image, string(sb.Status), sb.WorkspaceID, sb.WorkspacePath,
		meta, sb.TTLSeconds, nullTime(sb.ExpiresAt), sb.LastActiveAt, sb.UpdatedAt,
		sb.Name, sb.Category, sb.IsDefault, sb.CPUCount, sb.MemoryMB, sb.DiskSizeMB, sb.TemplateBuild, sb.Owner,
	)
	if err != nil {
		return mapUniqueViolation(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sandbox.ErrNotFound
	}
	return nil
}

// SoftDelete marks a sandbox deleted.
func (s *SandboxStore) SoftDelete(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.SQL.ExecContext(ctx, `
		UPDATE sandboxes SET deleted_at=$2, status=$3, updated_at=$2, is_default=false
		WHERE id=$1 AND deleted_at IS NULL`,
		id, at, string(sandbox.StatusStopped),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sandbox.ErrNotFound
	}
	return nil
}

// Get returns a non-deleted sandbox.
func (s *SandboxStore) Get(ctx context.Context, id string) (*sandbox.Sandbox, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT `+sandboxCols+`
		FROM sandboxes WHERE id=$1 AND deleted_at IS NULL`, id)
	return scanSandbox(row)
}

// GetByName returns a sandbox by case-insensitive name.
func (s *SandboxStore) GetByName(ctx context.Context, name, owner string) (*sandbox.Sandbox, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, sandbox.ErrNotFound
	}
	q := `
		SELECT ` + sandboxCols + `
		FROM sandboxes WHERE deleted_at IS NULL AND lower(name)=lower($1)`
	args := []any{name}
	if owner != "" {
		q += ` AND owner=$2`
		args = append(args, owner)
	}
	row := s.db.SQL.QueryRowContext(ctx, q, args...)
	return scanSandbox(row)
}

// GetDefaultByCategory returns the default sandbox in a category, if any.
func (s *SandboxStore) GetDefaultByCategory(ctx context.Context, category, owner string) (*sandbox.Sandbox, error) {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil, sandbox.ErrNotFound
	}
	q := `
		SELECT ` + sandboxCols + `
		FROM sandboxes
		WHERE deleted_at IS NULL AND is_default AND lower(category)=lower($1)`
	args := []any{category}
	if owner != "" {
		q += ` AND owner=$2`
		args = append(args, owner)
	}
	row := s.db.SQL.QueryRowContext(ctx, q, args...)
	return scanSandbox(row)
}

// List returns non-deleted sandboxes, newest first.
func (s *SandboxStore) List(ctx context.Context) ([]*sandbox.Sandbox, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT `+sandboxCols+`
		FROM sandboxes WHERE deleted_at IS NULL
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	return scanSandboxRows(rows)
}

// ListByCategory returns sandboxes in a category (case-insensitive), newest first.
func (s *SandboxStore) ListByCategory(ctx context.Context, category string) ([]*sandbox.Sandbox, error) {
	category = strings.TrimSpace(category)
	if category == "" {
		return s.List(ctx)
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT `+sandboxCols+`
		FROM sandboxes
		WHERE deleted_at IS NULL AND lower(category)=lower($1)
		ORDER BY is_default DESC, last_active_at DESC, created_at DESC`, category)
	if err != nil {
		return nil, err
	}
	return scanSandboxRows(rows)
}

// ClearDefaultInCategory clears is_default for other sandboxes in the category.
func (s *SandboxStore) ClearDefaultInCategory(ctx context.Context, category, exceptID, owner string) error {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil
	}
	q := `
		UPDATE sandboxes SET is_default=false, updated_at=now()
		WHERE deleted_at IS NULL AND is_default AND lower(category)=lower($1) AND id<>$2`
	args := []any{category, exceptID}
	if owner != "" {
		q += ` AND owner=$3`
		args = append(args, owner)
	}
	_, err := s.db.SQL.ExecContext(ctx, q, args...)
	return err
}

func scanSandboxRows(rows *sql.Rows) ([]*sandbox.Sandbox, error) {
	defer rows.Close()
	var out []*sandbox.Sandbox
	for rows.Next() {
		sb, err := scanSandbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sb)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSandbox(row scannable) (*sandbox.Sandbox, error) {
	var (
		sb      sandbox.Sandbox
		status  string
		metaRaw []byte
		expires sql.NullTime
	)
	err := row.Scan(
		&sb.ID, &sb.ContainerID, &sb.Image, &status, &sb.WorkspaceID, &sb.WorkspacePath,
		&metaRaw, &sb.TTLSeconds, &expires, &sb.LastActiveAt, &sb.CreatedAt, &sb.UpdatedAt,
		&sb.Name, &sb.Category, &sb.IsDefault, &sb.CPUCount, &sb.MemoryMB, &sb.DiskSizeMB, &sb.TemplateBuild, &sb.Owner,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sandbox.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sb.Status = sandbox.Status(status)
	if expires.Valid {
		t := expires.Time.UTC()
		sb.ExpiresAt = &t
	}
	sb.Metadata = map[string]string{}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &sb.Metadata)
	}
	return &sb, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func mapUniqueViolation(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return sandbox.ErrConflict
	}
	return err
}
