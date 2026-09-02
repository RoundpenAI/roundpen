package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Store persists app settings in PostgreSQL.
type Store struct {
	sql *sql.DB
}

// NewStore returns a settings store.
func NewStore(db *sql.DB) *Store {
	return &Store{sql: db}
}

// Exists reports whether the global settings row is present.
func (s *Store) Exists(ctx context.Context) (bool, error) {
	var n int
	err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE id=$1`, globalID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Get loads persisted settings.
func (s *Store) Get(ctx context.Context) (AppSettings, error) {
	return s.Load(ctx, AppSettings{})
}

// Load decodes the global row, filling missing keys from fallback.
func (s *Store) Load(ctx context.Context, fallback AppSettings) (AppSettings, error) {
	var raw []byte
	err := s.sql.QueryRowContext(ctx, `SELECT payload FROM app_settings WHERE id=$1`, globalID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return AppSettings{}, fmt.Errorf("settings not found")
	}
	if err != nil {
		return AppSettings{}, err
	}
	return DecodeAppSettings(raw, fallback)
}

// Upsert saves settings.
func (s *Store) Upsert(ctx context.Context, settings AppSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.sql.ExecContext(ctx, `
		INSERT INTO app_settings (id, payload, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE SET payload=EXCLUDED.payload, updated_at=now()`,
		globalID, raw,
	)
	return err
}
