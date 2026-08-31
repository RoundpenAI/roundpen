package template

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func testStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ROUNDPEN_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL to a dedicated test database (make dev configures roundpen_test)")
	}
	if isDevRoundpenDSN(dsn) && !allowDevDBTests() {
		t.Skip("refusing template tests on dev roundpen database; use ROUNDPEN_TEST_DATABASE_URL=.../roundpen_test")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("postgres open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("postgres ping: %v", err)
	}
	schemaSQL, err := readRepoSchemaSQL()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewStore(db), db
}

func allowDevDBTests() bool {
	return strings.TrimSpace(os.Getenv("ROUNDPEN_TEST_ALLOW_DEV_DB")) == "1"
}

func isDevRoundpenDSN(dsn string) bool {
	u, err := url.Parse(dsn)
	if err != nil {
		return strings.Contains(dsn, "/roundpen?") || strings.HasSuffix(dsn, "/roundpen")
	}
	db := strings.TrimPrefix(u.Path, "/")
	return db == "roundpen"
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
	res, err := db.Exec(`DELETE FROM templates WHERE namespace=$1 AND name=$2`, ns, name)
	if err != nil {
		t.Errorf("cleanup delete %s/%s: %v", ns, name, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		t.Logf("cleanup: no template deleted for %s/%s", ns, name)
	}
}
