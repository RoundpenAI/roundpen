package auth

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBootstrapAdminPinsConfiguredPassword(t *testing.T) {
	ctx := context.Background()
	users := storage.NewMemoryUserStore()
	credFile := filepath.Join(t.TempDir(), "bootstrap-admin-credentials.txt")

	if err := BootstrapAdmin(ctx, users, "rp-test-key", "RoundpenAdmin1", credFile, quietLogger()); err != nil {
		t.Fatal(err)
	}
	admin, err := users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(admin.PasswordHash, "RoundpenAdmin1") {
		t.Fatal("configured password not applied on first boot")
	}

	changed, err := HashPassword("changedByOperator1")
	if err != nil {
		t.Fatal(err)
	}
	admin.PasswordHash = changed
	if err := users.Upsert(ctx, *admin); err != nil {
		t.Fatal(err)
	}
	if err := BootstrapAdmin(ctx, users, "rp-test-key", "RoundpenAdmin1", credFile, quietLogger()); err != nil {
		t.Fatal(err)
	}
	admin, err = users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(admin.PasswordHash, "RoundpenAdmin1") {
		t.Fatal("pinned password not restored on the next boot")
	}

	if b, err := os.ReadFile(credFile); err == nil && strings.Contains(string(b), "RoundpenAdmin1") {
		t.Fatalf("pinned password must not land in the credentials file: %q", string(b))
	}
}

func TestBootstrapAdminRejectsShortConfiguredPassword(t *testing.T) {
	ctx := context.Background()
	users := storage.NewMemoryUserStore()
	err := BootstrapAdmin(ctx, users, "rp-test-key", "short", filepath.Join(t.TempDir(), "creds.txt"), quietLogger())
	if err == nil {
		t.Fatal("expected an error for a password below the minimum length")
	}
}

func TestBootstrapAdminGeneratesPasswordWithoutConfig(t *testing.T) {
	ctx := context.Background()
	users := storage.NewMemoryUserStore()
	credFile := filepath.Join(t.TempDir(), "creds.txt")

	if err := BootstrapAdmin(ctx, users, "rp-test-key", "", credFile, quietLogger()); err != nil {
		t.Fatal(err)
	}
	admin, err := users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.PasswordHash == "" {
		t.Fatal("expected a generated password hash")
	}
	b, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("credentials file: %v", err)
	}
	if !strings.Contains(string(b), "admin initial password: ") {
		t.Fatalf("generated password not recorded: %q", string(b))
	}
}
