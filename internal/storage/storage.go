// Package storage holds PostgreSQL connectivity and shared persistence helpers.
package storage

import (
	"context"
	"fmt"
)

// DB is the narrow interface roundpend needs from a SQL driver.
// Concrete pgx wiring is added when migrations land.
type DB interface {
	Ping(ctx context.Context) error
	Close() error
}

// OpenPostgres is a placeholder until pgx is wired in Phase 1.
func OpenPostgres(ctx context.Context, databaseURL string) (DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return nil, fmt.Errorf("storage.OpenPostgres: not implemented (wire pgx next)")
}
