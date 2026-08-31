package template

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a template or build cannot be resolved.
var ErrNotFound = errors.New("template not found")

// Store persists templates and builds.
type Store struct {
	db *sql.DB
}

// NewStore returns a template store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

type seedEntry struct {
	Namespace   string
	Name        string
	Description string
	Profile     string
	ArtifactRef string
	BaseImage   string
	CPUCount    int
	MemoryMB    int
	DiskSizeMB  int
	Public      bool
}

// SeedBuiltin inserts built-in templates idempotently.
func (s *Store) SeedBuiltin(ctx context.Context, backend, defaultImage string) error {
	entries := builtinEntries(backend, defaultImage)
	for _, e := range entries {
		if err := s.upsertSeed(ctx, e); err != nil {
			return fmt.Errorf("seed %s/%s: %w", e.Namespace, e.Name, err)
		}
	}
	return nil
}

func (s *Store) upsertSeed(ctx context.Context, e seedEntry) error {
	var tplID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM templates WHERE namespace=$1 AND name=$2`, e.Namespace, e.Name).Scan(&tplID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		tplID = uuid.NewString()
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO templates (id, namespace, name, description, profile, public, build_count, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,1,'system')`,
			tplID, e.Namespace, e.Name, e.Description, e.Profile, e.Public)
		if err != nil {
			return err
		}
		buildID := uuid.NewString()
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO template_builds (id, template_id, status, base_image, artifact_ref, cpu_count, memory_mb, disk_size_mb, envd_version)
			VALUES ($1,$2,'ready',$3,$4,$5,$6,$7,$8)`,
			buildID, tplID, e.BaseImage, e.ArtifactRef, e.CPUCount, e.MemoryMB, e.DiskSizeMB, EnvdVersion); err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO template_tags (template_id, tag, build_id) VALUES ($1,'default',$2)
			ON CONFLICT (template_id, tag) DO UPDATE SET build_id=EXCLUDED.build_id`,
			tplID, buildID)
		return err
	case err != nil:
		return err
	default:
		return nil
	}
}

// List returns all templates with their default-tag build.
func (s *Store) List(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.namespace, t.name, t.description, t.profile, t.public,
			t.spawn_count, t.build_count, t.last_spawned_at, t.created_by, t.created_at, t.updated_at,
			b.id, b.status, b.artifact_ref, b.cpu_count, b.memory_mb, b.disk_size_mb, b.envd_version
		FROM templates t
		LEFT JOIN template_tags tg ON tg.template_id=t.id AND tg.tag='default'
		LEFT JOIN template_builds b ON b.id=tg.build_id
		ORDER BY t.namespace, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ResolveByTag finds a build by namespace, name, and tag.
func (s *Store) ResolveByTag(ctx context.Context, ref ParsedRef) (Resolved, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.namespace, t.name, t.profile,
			b.id, b.artifact_ref, b.cpu_count, b.memory_mb, b.disk_size_mb,
			b.start_cmd, b.snapshot
		FROM templates t
		JOIN template_tags tg ON tg.template_id=t.id AND tg.tag=$3
		JOIN template_builds b ON b.id=tg.build_id AND b.status='ready'
		WHERE t.namespace=$1 AND t.name=$2`,
		ref.Namespace, ref.Name, ref.Tag)
	return scanResolved(row, ref)
}

// ResolveByBuildID finds a build by UUID.
func (s *Store) ResolveByBuildID(ctx context.Context, buildID string) (Resolved, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.namespace, t.name, t.profile,
			b.id, b.artifact_ref, b.cpu_count, b.memory_mb, b.disk_size_mb,
			b.start_cmd, b.snapshot
		FROM template_builds b
		JOIN templates t ON t.id=b.template_id
		WHERE b.id=$1 AND b.status='ready'`, buildID)
	return scanResolved(row, ParsedRef{BuildID: buildID})
}

// RecordSpawn increments spawn stats for a template.
func (s *Store) RecordSpawn(ctx context.Context, templateID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE templates SET spawn_count=spawn_count+1, last_spawned_at=now(), updated_at=now()
		WHERE id=$1`, templateID)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanResolved(row rowScanner, ref ParsedRef) (Resolved, error) {
	var (
		tplID, ns, name, profile string
		buildID, artifact        string
		cpu, mem, disk           int
		startCmd                 sql.NullString
		snapshot                 bool
	)
	err := row.Scan(&tplID, &ns, &name, &profile, &buildID, &artifact, &cpu, &mem, &disk, &startCmd, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return Resolved{}, ErrNotFound
	}
	if err != nil {
		return Resolved{}, err
	}
	alias := DisplayName(ns, name)
	if ref.Tag != "" && ref.Tag != DefaultTag {
		alias = alias + ":" + ref.Tag
	}
	if ref.BuildID != "" {
		alias = ref.BuildID
	}
	res := Resolved{
		TemplateID: tplID,
		BuildID:    buildID,
		Alias:      alias,
		Image:      artifact,
		Profile:    profile,
		CPUCount:   cpu,
		MemoryMB:   mem,
		DiskSizeMB: disk,
		Snapshot:   snapshot,
		UseImageCmd: snapshot,
	}
	if startCmd.Valid {
		res.StartCmd = startCmd.String
	}
	return res, nil
}

func scanRecord(row rowScanner) (Record, error) {
	var (
		rec                      Record
		status                   sql.NullString
		buildID, artifact, envd  sql.NullString
		cpu, mem, disk           sql.NullInt64
		lastSpawn                sql.NullTime
	)
	err := row.Scan(
		&rec.TemplateID, &rec.Namespace, &rec.Name, &rec.Description, &rec.Profile, &rec.Public,
		&rec.SpawnCount, &rec.BuildCount, &lastSpawn, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt,
		&buildID, &status, &artifact, &cpu, &mem, &disk, &envd,
	)
	if err != nil {
		return Record{}, err
	}
	if buildID.Valid {
		rec.BuildID = buildID.String
	}
	if status.Valid {
		rec.BuildStatus = BuildStatus(status.String)
	} else {
		rec.BuildStatus = BuildError
	}
	if cpu.Valid {
		rec.CPUCount = int(cpu.Int64)
	} else {
		rec.CPUCount = 1
	}
	if mem.Valid {
		rec.MemoryMB = int(mem.Int64)
	} else {
		rec.MemoryMB = 512
	}
	if disk.Valid {
		rec.DiskSizeMB = int(disk.Int64)
	} else {
		rec.DiskSizeMB = 5120
	}
	if envd.Valid {
		rec.EnvdVersion = envd.String
	} else {
		rec.EnvdVersion = EnvdVersion
	}
	if lastSpawn.Valid {
		t := lastSpawn.Time.UTC()
		rec.LastSpawnedAt = &t
	}
	display := DisplayName(rec.Namespace, rec.Name)
	rec.Names = []string{display}
	rec.Aliases = []string{display}
	return rec, nil
}

func builtinEntries(backend, defaultImage string) []seedEntry {
	hostArtifact := defaultImage
	if hostArtifact == "" {
		hostArtifact = "host"
	}
	entries := []seedEntry{
		{
			Namespace: DefaultNamespace, Name: "host",
			Description: "Kern host environment (local dev default)",
			Profile:     "dev", ArtifactRef: "host", BaseImage: "host",
			CPUCount: 1, MemoryMB: 512, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "base",
			Description: "Minimal Ubuntu 22.04",
			Profile:     "shell", ArtifactRef: "ubuntu:22.04", BaseImage: "ubuntu:22.04",
			CPUCount: 1, MemoryMB: 512, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "python",
			Description: "Python 3.12 for code agents",
			Profile:     "dev", ArtifactRef: "python:3.12-slim", BaseImage: "python:3.12-slim",
			CPUCount: 1, MemoryMB: 1024, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "node",
			Description: "Node.js 22 for JS/TS agents",
			Profile:     "dev", ArtifactRef: "node:22-bookworm", BaseImage: "node:22-bookworm",
			CPUCount: 1, MemoryMB: 1024, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "code-agent",
			Description: "Ubuntu with git/curl for coding agents",
			Profile:     "dev", ArtifactRef: "ubuntu:22.04", BaseImage: "ubuntu:22.04",
			CPUCount: 2, MemoryMB: 2048, DiskSizeMB: 10240, Public: true,
		},
	}
	if backend == "kern" {
		// Kern uses host jail; docker-only images are listed but host stays primary.
		entries[0].ArtifactRef = "host"
	} else if backend == "docker" && hostArtifact != "host" {
		entries[0].ArtifactRef = hostArtifact
		entries[0].BaseImage = hostArtifact
	}
	return entries
}
