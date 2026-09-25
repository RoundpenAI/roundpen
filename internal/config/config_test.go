package config_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestLoadIMEnabled(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://roundpen:roundpen@127.0.0.1:5432/roundpen?sslmode=disable")
	t.Setenv("ROUNDPEN_BACKEND", "docker")

	t.Setenv("ROUNDPEN_IM_ENABLED", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IMEnabled {
		t.Fatal("IM engines must stay off when the variable is unset")
	}

	t.Setenv("ROUNDPEN_IM_ENABLED", "true")
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IMEnabled {
		t.Fatal("ROUNDPEN_IM_ENABLED=true must enable the IM engines")
	}
}
