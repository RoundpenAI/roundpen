package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
)

// Store persists app settings in PostgreSQL.
// Secret fields are sealed at the storage boundary (AES-GCM, "enc:v1:"
// prefix); legacy plaintext rows stay readable and are re-encrypted on the
// next Upsert. A nil box disables encryption (tests, single-user dev).
type Store struct {
	sql *sql.DB
	box *secretbox.Box
}

// NewStore returns a settings store.
func NewStore(db *sql.DB, box *secretbox.Box) *Store {
	return &Store{sql: db, box: box}
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
	out, err := DecodeAppSettings(raw, fallback)
	if err != nil {
		return AppSettings{}, err
	}
	if s.box != nil {
		s.openSecrets(&out)
	}
	return out, nil
}

// Upsert saves settings.
func (s *Store) Upsert(ctx context.Context, settings AppSettings) error {
	if s.box != nil {
		sealed, err := s.sealSecrets(settings)
		if err != nil {
			return err
		}
		settings = sealed
	}
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

// secretFields lists every field that must never be persisted in plaintext.
// Keep in sync with SanitizeForResponse / MergeSecrets.
func secretFields(s *AppSettings) []*string {
	// Provider credentials and the CDP token moved to setting items, which seal
	// their own secret fields.
	return []*string{&s.LlmgwVirtualKeys}
}

func (s *Store) sealSecrets(in AppSettings) (AppSettings, error) {
	out := in
	for _, f := range secretFields(&out) {
		sealed, err := s.box.Seal(*f)
		if err != nil {
			return in, err
		}
		*f = sealed
	}
	return out, nil
}

// openSecrets decrypts in place. A field that fails to decrypt (e.g. the
// master key was regenerated) is blanked with a warning so the rest of the
// settings stay loadable; the admin re-enters the affected secret.
func (s *Store) openSecrets(in *AppSettings) {
	for _, f := range secretFields(in) {
		plain, err := s.box.Open(*f)
		if err != nil {
			slog.Warn("settings: secret decryption failed; blanking field", "err", err)
			*f = ""
			continue
		}
		*f = plain
	}
}
