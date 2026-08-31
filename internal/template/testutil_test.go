package template

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func testStore(t *testing.T) (*Store, *sql.DB, func()) {
	t.Helper()
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("postgres open: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("postgres ping: %v", err)
	}
	schemaSQL, err := readRepoSchemaSQL()
	if err != nil {
		_ = db.Close()
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}
	return NewStore(db), db, func() { _ = db.Close() }
}

func readRepoSchemaSQL() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, "internal/storage/schema/postgres.sql")
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func deleteTemplateByName(t *testing.T, db *sql.DB, ns, name string) {
	t.Helper()
	_, _ = db.Exec(`DELETE FROM templates WHERE namespace=$1 AND name=$2`, ns, name)
}
