package template

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a template cannot be resolved.
var ErrNotFound = errors.New("template not found")

// Store persists the image catalog.
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
	Slot        string
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
	if e.Slot == "" {
		e.Slot = SlotFromProfile(e.Profile)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO templates (
			id, namespace, name, description, profile, slot, public, build_count, created_by,
			artifact_ref, base_image, cpu_count, memory_mb, disk_size_mb, envd_version
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,1,'system',$8,$9,$10,$11,$12,$13)
		ON CONFLICT (namespace, name) DO UPDATE SET
			description=EXCLUDED.description,
			profile=EXCLUDED.profile,
			slot=EXCLUDED.slot,
			public=EXCLUDED.public,
			artifact_ref=EXCLUDED.artifact_ref,
			base_image=EXCLUDED.base_image,
			cpu_count=EXCLUDED.cpu_count,
			memory_mb=EXCLUDED.memory_mb,
			disk_size_mb=EXCLUDED.disk_size_mb,
			envd_version=EXCLUDED.envd_version,
			updated_at=now()`,
		uuid.NewString(), e.Namespace, e.Name, e.Description, e.Profile, e.Slot, e.Public,
		e.ArtifactRef, e.BaseImage, e.CPUCount, e.MemoryMB, e.DiskSizeMB, EnvdVersion)
	return err
}

const templateCols = `id, namespace, name, description, profile, slot, public,
	spawn_count, build_count, last_spawned_at, created_by, created_at, updated_at,
	cpu_count, memory_mb, disk_size_mb, envd_version`

// List returns all registered templates.
func (s *Store) List(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+templateCols+` FROM templates ORDER BY namespace, name`)
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

// ResolveByName finds a template in the default or a named namespace.
func (s *Store) ResolveByName(ctx context.Context, ref ParsedRef) (Resolved, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, namespace, name, profile, slot,
			artifact_ref, cpu_count, memory_mb, disk_size_mb, start_cmd, snapshot
		FROM templates
		WHERE namespace=$1 AND name=$2`,
		ref.Namespace, ref.Name)
	return scanResolved(row)
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

func scanResolved(row rowScanner) (Resolved, error) {
	var (
		tplID, ns, name, profile, slot string
		artifact                       string
		cpu, mem, disk                 int
		startCmd                       sql.NullString
		snapshot                       bool
	)
	err := row.Scan(&tplID, &ns, &name, &profile, &slot, &artifact, &cpu, &mem, &disk, &startCmd, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return Resolved{}, ErrNotFound
	}
	if err != nil {
		return Resolved{}, err
	}
	if slot == "" {
		slot = SlotFromProfile(profile)
	}
	res := Resolved{
		TemplateID:  tplID,
		Alias:       DisplayName(ns, name),
		Image:       artifact,
		Profile:     profile,
		Slot:        slot,
		CPUCount:    cpu,
		MemoryMB:    mem,
		DiskSizeMB:  disk,
		Snapshot:    snapshot,
		UseImageCmd: snapshot,
	}
	if startCmd.Valid {
		res.StartCmd = startCmd.String
	}
	return res, nil
}

func scanRecord(row rowScanner) (Record, error) {
	var (
		rec       Record
		lastSpawn sql.NullTime
	)
	err := row.Scan(
		&rec.TemplateID, &rec.Namespace, &rec.Name, &rec.Description, &rec.Profile, &rec.Slot, &rec.Public,
		&rec.SpawnCount, &rec.BuildCount, &lastSpawn, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt,
		&rec.CPUCount, &rec.MemoryMB, &rec.DiskSizeMB, &rec.EnvdVersion,
	)
	if err != nil {
		return Record{}, err
	}
	if rec.Slot == "" {
		rec.Slot = SlotFromProfile(rec.Profile)
	}
	// Kept for existing API consumers: every catalog entry is ready, and
	// build identity no longer exists.
	rec.BuildStatus = BuildReady
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
	// Official Agent image: overridable for offline/private registries.
	agentArtifact := getenv("ROUNDPEN_AGENT_IMAGE", "ghcr.io/roundpenai/code-agent:0.1.0")
	browserArtifact := getenv("ROUNDPEN_BROWSER_IMAGE", "ghcr.io/browserless/chrome:v2.56.7")
	return []seedEntry{
		{
			Namespace: DefaultNamespace, Name: "base",
			Description: "Minimal Ubuntu 22.04",
			Profile:     "shell", Slot: "agent", ArtifactRef: "ubuntu:22.04", BaseImage: "ubuntu:22.04",
			CPUCount: 1, MemoryMB: 512, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "python",
			Description: "Python 3.12 for code agents",
			Profile:     "dev", Slot: "agent", ArtifactRef: "python:3.12-slim", BaseImage: "python:3.12-slim",
			CPUCount: 1, MemoryMB: 1024, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "node",
			Description: "Node.js 22 for JS/TS agents",
			Profile:     "dev", Slot: "agent", ArtifactRef: "node:22-bookworm", BaseImage: "node:22-bookworm",
			CPUCount: 1, MemoryMB: 1024, DiskSizeMB: 5120, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "code-agent",
			Description: "Ubuntu with git/curl/openssh for coding agents",
			Profile:     "dev", Slot: "agent",
			ArtifactRef: agentArtifact, BaseImage: "ubuntu:22.04",
			CPUCount: 2, MemoryMB: 2048, DiskSizeMB: 10240, Public: true,
		},
		{
			Namespace: DefaultNamespace, Name: "browser",
			Description: "Browserless Chrome container (CDP :3000, live debugger)",
			Profile:     "browser", Slot: "browser",
			ArtifactRef: browserArtifact, BaseImage: browserArtifact,
			CPUCount: 2, MemoryMB: 2048, DiskSizeMB: 5120, Public: true,
		},
	}
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
