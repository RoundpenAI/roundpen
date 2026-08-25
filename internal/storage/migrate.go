package storage

import (
	"context"
	_ "embed"
)

//go:embed schema/postgres.sql
var schemaSQL string

// MigrateEmbedded applies the embedded schema.
func (db *DB) MigrateEmbedded(ctx context.Context) error {
	_, err := db.SQL.ExecContext(ctx, schemaSQL)
	return err
}
