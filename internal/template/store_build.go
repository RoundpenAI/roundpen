package template

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CreateTemplate inserts a template and a waiting build row.
func (s *Store) CreateTemplate(ctx context.Context, req CreateTemplateRequest) (CreateTemplateResult, error) {
	ns := req.Namespace
	if ns == "" {
		ns = DefaultNamespace
	}
	name := req.Name
	if !ValidateName(name) {
		return CreateTemplateResult{}, fmt.Errorf("invalid template name %q", name)
	}
	profile := req.Profile
	if profile == "" {
		profile = "dev"
	}
	cpu := req.CPUCount
	if cpu <= 0 {
		cpu = 1
	}
	mem := req.MemoryMB
	if mem <= 0 {
		mem = 512
	}

	tplID := uuid.NewString()
	buildID := uuid.NewString()
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateTemplateResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM templates WHERE namespace=$1 AND name=$2`, ns, name).Scan(&existing)
	if err == nil {
		return CreateTemplateResult{}, fmt.Errorf("template already exists")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateTemplateResult{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO templates (id, namespace, name, profile, public, build_count, created_by)
		VALUES ($1,$2,$3,$4,$5,0,$6)`,
		tplID, ns, name, profile, req.Public, req.CreatedBy)
	if err != nil {
		return CreateTemplateResult{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO template_builds (id, template_id, status, cpu_count, memory_mb, disk_size_mb, envd_version)
		VALUES ($1,$2,'waiting',$3,$4,5120,$5)`,
		buildID, tplID, cpu, mem, EnvdVersion)
	if err != nil {
		return CreateTemplateResult{}, err
	}

	tags := req.Tags
	if len(tags) == 0 && req.Tag != "" {
		tags = []string{req.Tag}
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO template_tags (template_id, tag, build_id) VALUES ($1,$2,$3)
			ON CONFLICT (template_id, tag) DO UPDATE SET build_id=EXCLUDED.build_id`,
			tplID, tag, buildID)
		if err != nil {
			return CreateTemplateResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return CreateTemplateResult{}, err
	}
	_ = now
	return CreateTemplateResult{
		TemplateID: tplID,
		BuildID:    buildID,
		Namespace:  ns,
		Name:       name,
		Tags:       tags,
		Public:     req.Public,
	}, nil
}

// GetBuild returns build metadata.
func (s *Store) GetBuild(ctx context.Context, templateID, buildID string) (BuildInfo, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, template_id, status, artifact_ref, cache_key, spec_json,
			start_cmd, ready_cmd, snapshot, error_message, cpu_count, memory_mb, disk_size_mb
		FROM template_builds WHERE id=$1 AND template_id=$2`, buildID, templateID)
	return scanBuildInfo(row)
}

// FindCachedBuild returns a ready build with the same cache key.
func (s *Store) FindCachedBuild(ctx context.Context, templateID, cacheKey string) (BuildInfo, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, template_id, status, artifact_ref, cache_key, spec_json,
			start_cmd, ready_cmd, snapshot, error_message, cpu_count, memory_mb, disk_size_mb
		FROM template_builds
		WHERE template_id=$1 AND cache_key=$2 AND status='ready' AND artifact_ref<>''
		ORDER BY updated_at DESC LIMIT 1`, templateID, cacheKey)
	return scanBuildInfo(row)
}

// UpdateBuildStatus sets build status and optional fields.
func (s *Store) UpdateBuildStatus(ctx context.Context, buildID string, status BuildStatus, artifact, cacheKey, errMsg string, snapshot bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE template_builds SET
			status=$2, artifact_ref=COALESCE(NULLIF($3,''), artifact_ref),
			cache_key=COALESCE(NULLIF($4,''), cache_key),
			error_message=$5, snapshot=$6, updated_at=now()
		WHERE id=$1`,
		buildID, status, artifact, cacheKey, errMsg, snapshot)
	return err
}

// SaveBuildSpec persists spec and layer metadata before build starts.
func (s *Store) SaveBuildSpec(ctx context.Context, buildID string, spec BuildSpec, cacheKey string) error {
	specRaw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	layersRaw, err := json.Marshal(spec.Steps)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE template_builds SET
			spec_json=$2, layers_json=$3, cache_key=$4,
			start_cmd=$5, ready_cmd=$6,
			cpu_count=CASE WHEN $7>0 THEN $7 ELSE cpu_count END,
			memory_mb=CASE WHEN $8>0 THEN $8 ELSE memory_mb END,
			status='building', updated_at=now()
		WHERE id=$1`,
		buildID, specRaw, layersRaw, cacheKey, spec.StartCmd, spec.ReadyCmd, spec.CPUCount, spec.MemoryMB)
	return err
}

// FinishBuild marks a build ready and updates template counters/tags.
func (s *Store) FinishBuild(ctx context.Context, templateID, buildID, artifact string, cacheKey string, snapshot bool, assignDefault bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		UPDATE template_builds SET status='ready', artifact_ref=$2, cache_key=$3,
			snapshot=$4, error_message='', updated_at=now()
		WHERE id=$1`, buildID, artifact, cacheKey, snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE templates SET build_count=build_count+1, updated_at=now() WHERE id=$1`, templateID)
	if err != nil {
		return err
	}
	if assignDefault {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO template_tags (template_id, tag, build_id) VALUES ($1,'default',$2)
			ON CONFLICT (template_id, tag) DO UPDATE SET build_id=EXCLUDED.build_id`,
			templateID, buildID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FailBuild marks build as error.
func (s *Store) FailBuild(ctx context.Context, buildID, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE template_builds SET status='error', error_message=$2, updated_at=now()
		WHERE id=$1`, buildID, message)
	return err
}

// AppendBuildLog adds a log entry.
func (s *Store) AppendBuildLog(ctx context.Context, buildID, level, step, message string) error {
	var seq int
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq),0)+1 FROM template_build_logs WHERE build_id=$1`, buildID).Scan(&seq)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO template_build_logs (build_id, seq, level, message, step) VALUES ($1,$2,$3,$4,$5)`,
		buildID, seq, level, message, step)
	return err
}

// ListBuildLogs returns log entries from offset.
func (s *Store) ListBuildLogs(ctx context.Context, buildID string, offset, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, logged_at, level, message, step
		FROM template_build_logs WHERE build_id=$1 AND seq>$2
		ORDER BY seq ASC LIMIT $3`, buildID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts time.Time
		if err := rows.Scan(&e.Seq, &ts, &e.Level, &e.Message, &e.Step); err != nil {
			return nil, err
		}
		e.Timestamp = ts.UTC().Format(time.RFC3339Nano)
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanBuildInfo(row rowScanner) (BuildInfo, error) {
	var (
		info                     BuildInfo
		status                   string
		specRaw, artifact, cache sql.NullString
		startCmd, readyCmd, errMsg sql.NullString
		snapshot                 bool
	)
	err := row.Scan(
		&info.BuildID, &info.TemplateID, &status, &artifact, &cache, &specRaw,
		&startCmd, &readyCmd, &snapshot, &errMsg, &info.CPUCount, &info.MemoryMB, &info.DiskSizeMB,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BuildInfo{}, ErrNotFound
	}
	if err != nil {
		return BuildInfo{}, err
	}
	info.Status = BuildStatus(status)
	if artifact.Valid {
		info.ArtifactRef = artifact.String
	}
	if cache.Valid {
		info.CacheKey = cache.String
	}
	if startCmd.Valid {
		info.StartCmd = startCmd.String
	}
	if readyCmd.Valid {
		info.ReadyCmd = readyCmd.String
	}
	if errMsg.Valid {
		info.ErrorMessage = errMsg.String
	}
	info.Snapshot = snapshot
	if len(specRaw.String) > 0 {
		_ = json.Unmarshal([]byte(specRaw.String), &info.Spec)
	}
	return info, nil
}
