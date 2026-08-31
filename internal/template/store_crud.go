package template

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrBuiltin is returned when a built-in template cannot be modified or deleted.
var ErrBuiltin = errors.New("built-in template is read-only")

// UpdateTemplateRequest patches mutable template fields.
type UpdateTemplateRequest struct {
	Description *string
	Public      *bool
	CPUCount    *int
	MemoryMB    *int
	DiskSizeMB  *int
}

// BuildSummary is a template build row for detail views.
type BuildSummary struct {
	BuildID      string
	Status       BuildStatus
	ArtifactRef  string
	CPUCount     int
	MemoryMB     int
	DiskSizeMB   int
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const templateSelectCols = `
	t.id, t.namespace, t.name, t.description, t.profile, t.public,
	t.spawn_count, t.build_count, t.last_spawned_at, t.created_by, t.created_at, t.updated_at,
	b.id, b.status, b.artifact_ref, b.cpu_count, b.memory_mb, b.disk_size_mb, b.envd_version`

const templateFromJoin = `
	FROM templates t
	LEFT JOIN template_tags tg ON tg.template_id=t.id AND tg.tag='default'
	LEFT JOIN template_builds b ON b.id=tg.build_id`

// GetByID returns one template with its default-tag build metadata.
func (s *Store) GetByID(ctx context.Context, templateID string) (Record, error) {
	row := s.db.QueryRowContext(ctx, `SELECT`+templateSelectCols+templateFromJoin+`
		WHERE t.id=$1`, templateID)
	rec, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return rec, err
}

// ListBuilds returns builds for a template, newest first.
func (s *Store) ListBuilds(ctx context.Context, templateID string) ([]BuildSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, artifact_ref, cpu_count, memory_mb, disk_size_mb,
			COALESCE(error_message,''), created_at, updated_at
		FROM template_builds
		WHERE template_id=$1
		ORDER BY created_at DESC`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BuildSummary
	for rows.Next() {
		var b BuildSummary
		var status string
		if err := rows.Scan(
			&b.BuildID, &status, &b.ArtifactRef, &b.CPUCount, &b.MemoryMB, &b.DiskSizeMB,
			&b.ErrorMessage, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, err
		}
		b.Status = BuildStatus(status)
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpdateTemplate patches template metadata and default build resources.
func (s *Store) UpdateTemplate(ctx context.Context, templateID string, req UpdateTemplateRequest) error {
	rec, err := s.GetByID(ctx, templateID)
	if err != nil {
		return err
	}
	if IsBuiltin(rec.Namespace, rec.Name, rec.CreatedBy) {
		return ErrBuiltin
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if req.Description != nil || req.Public != nil {
		desc := rec.Description
		if req.Description != nil {
			desc = *req.Description
		}
		public := rec.Public
		if req.Public != nil {
			public = *req.Public
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE templates SET description=$2, public=$3, updated_at=now()
			WHERE id=$1`, templateID, desc, public)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}

	buildID := rec.BuildID
	if buildID == "" {
		err = tx.QueryRowContext(ctx, `
			SELECT build_id FROM template_tags WHERE template_id=$1 AND tag='default'`, templateID).Scan(&buildID)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, `
				SELECT id FROM template_builds WHERE template_id=$1 ORDER BY created_at DESC LIMIT 1`, templateID).Scan(&buildID)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return tx.Commit()
			}
			return err
		}
	}

	if req.CPUCount != nil || req.MemoryMB != nil || req.DiskSizeMB != nil {
		cpu := rec.CPUCount
		mem := rec.MemoryMB
		disk := rec.DiskSizeMB
		if req.CPUCount != nil && *req.CPUCount > 0 {
			cpu = *req.CPUCount
		}
		if req.MemoryMB != nil && *req.MemoryMB > 0 {
			mem = *req.MemoryMB
		}
		if req.DiskSizeMB != nil && *req.DiskSizeMB > 0 {
			disk = *req.DiskSizeMB
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE template_builds SET cpu_count=$2, memory_mb=$3, disk_size_mb=$4, updated_at=now()
			WHERE id=$1`, buildID, cpu, mem, disk)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteTemplate removes a non-built-in template and cascaded builds.
func (s *Store) DeleteTemplate(ctx context.Context, templateID string) error {
	rec, err := s.GetByID(ctx, templateID)
	if err != nil {
		return err
	}
	if IsBuiltin(rec.Namespace, rec.Name, rec.CreatedBy) {
		return ErrBuiltin
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id=$1`, templateID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (req UpdateTemplateRequest) Validate() error {
	if req.CPUCount != nil && *req.CPUCount <= 0 {
		return fmt.Errorf("cpuCount must be positive")
	}
	if req.MemoryMB != nil && *req.MemoryMB <= 0 {
		return fmt.Errorf("memoryMB must be positive")
	}
	if req.DiskSizeMB != nil && *req.DiskSizeMB <= 0 {
		return fmt.Errorf("diskSizeMB must be positive")
	}
	return nil
}
