package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestLoadDotEnv(t *testing.T) {
	keys := []string{
		"ROUNDPEN_DOTENV_BASIC",
		"ROUNDPEN_DOTENV_QUOTED",
		"ROUNDPEN_DOTENV_SINGLE",
		"ROUNDPEN_DOTENV_EXPORTED",
		"ROUNDPEN_DOTENV_EMPTY",
		"ROUNDPEN_DOTENV_KEEP",
	}
	// t.Setenv both guarantees the precondition and restores the keys afterwards
	// (LoadDotEnv writes through os.Setenv, which cleanup does not undo).
	for _, k := range keys {
		t.Setenv(k, "")
	}
	t.Setenv("ROUNDPEN_DOTENV_KEEP", "from-env")

	path := filepath.Join(t.TempDir(), ".env")
	writeEnv(t, path, `
# comment line
ROUNDPEN_DOTENV_BASIC=plain

ROUNDPEN_DOTENV_QUOTED="quoted value"
ROUNDPEN_DOTENV_SINGLE='single value'
export ROUNDPEN_DOTENV_EXPORTED=exported
ROUNDPEN_DOTENV_EMPTY=from-file
ROUNDPEN_DOTENV_KEEP=from-file
`)
	if err := config.LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	for k, want := range map[string]string{
		"ROUNDPEN_DOTENV_BASIC":    "plain",
		"ROUNDPEN_DOTENV_QUOTED":   "quoted value",
		"ROUNDPEN_DOTENV_SINGLE":   "single value",
		"ROUNDPEN_DOTENV_EXPORTED": "exported",
		"ROUNDPEN_DOTENV_EMPTY":    "from-file",
		"ROUNDPEN_DOTENV_KEEP":     "from-env",
	} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := config.LoadDotEnv(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Fatalf("a missing file must be ignored, got %v", err)
	}
}

func TestLoadDotEnvMalformedLine(t *testing.T) {
	t.Setenv("ROUNDPEN_DOTENV_OK", "")

	path := filepath.Join(t.TempDir(), ".env")
	writeEnv(t, path, "ROUNDPEN_DOTENV_OK=1\nnot-a-pair\n")
	err := config.LoadDotEnv(path)
	if err == nil {
		t.Fatal("expected an error for a line without '='")
	}
	if !strings.Contains(err.Error(), ":2:") {
		t.Fatalf("error should point at line 2, got %v", err)
	}
}

func writeEnv(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
