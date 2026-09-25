package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
)

//go:embed schema/postgres.sql
var schemaSQL string

// migrateLockKey namespaces the advisory lock that serializes migrations. Any
// value works as long as every migrator of this database uses the same one.
const migrateLockKey int64 = 0x726f756e6470656e // "roundpen"

const migrateVersionID = "embedded"

// MigrateEmbedded applies the embedded schema, skipping the work once this
// exact file has already been applied.
//
// The schema is written to be idempotent so it can be replayed, but replaying
// it is not free: every statement takes locks on the tables it touches, so a
// concurrent writer deadlocks against it (two daemons on one database, or two
// test packages sharing a test database). Recording the file's hash and
// returning early keeps the replay path for upgrades — where the file changed —
// without the lock storm on every boot. The trade-off: a database modified by
// hand, outside this file, is no longer repaired on the next start.
func (db *DB) MigrateEmbedded(ctx context.Context) error {
	sum := sha256.Sum256([]byte(schemaSQL))
	want := hex.EncodeToString(sum[:])

	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize before any DDL: the first migrator applies, the rest see the new
	// hash and return. The bookkeeping table is created inside this lock, so two
	// first-time migrators cannot race on its creation.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrateLockKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id         TEXT PRIMARY KEY,
			hash       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return err
	}

	var have string
	err = tx.QueryRowContext(ctx,
		`SELECT hash FROM schema_migrations WHERE id = $1`, migrateVersionID).Scan(&have)
	switch {
	case err == nil && have == want:
		return tx.Commit()
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO schema_migrations (id, hash, applied_at) VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE SET hash = EXCLUDED.hash, applied_at = now()`,
		migrateVersionID, want); err != nil {
		return err
	}
	return tx.Commit()
}
