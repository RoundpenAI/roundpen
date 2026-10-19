package config_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestCoerceDefaultImage(t *testing.T) {
	const agent = "ghcr.io/roundpenai/code-agent:0.1.0"
	if got := config.CoerceDefaultImage("host", agent); got != agent {
		t.Fatalf("host: %q", got)
	}
	if got := config.CoerceDefaultImage("disks/agent.qcow2", agent); got != agent {
		t.Fatalf("qcow2: %q", got)
	}
	if got := config.CoerceDefaultImage("python:3.12-slim", agent); got != "python:3.12-slim" {
		t.Fatalf("real image: %q", got)
	}
	if config.PullableImage("host") || config.PullableImage("vm.qcow2") {
		t.Fatal("legacy refs must not be pulled")
	}
}

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

func TestLoadPreviewDomain(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://roundpen:roundpen@127.0.0.1:5432/roundpen?sslmode=disable")

	t.Setenv("ROUNDPEN_PREVIEW_DOMAIN", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PreviewDomain != "" || cfg.PreviewDomainScheme != "" {
		t.Fatalf("unset domain must stay off, got %q %q", cfg.PreviewDomain, cfg.PreviewDomainScheme)
	}

	// A scheme pins the origin the reverse proxy terminates; without one the
	// links follow the console request.
	t.Setenv("ROUNDPEN_PREVIEW_DOMAIN", "https://RP.mk")
	if cfg, err = config.Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.PreviewDomain != "rp.mk" || cfg.PreviewDomainScheme != "https" {
		t.Fatalf("got %q %q", cfg.PreviewDomain, cfg.PreviewDomainScheme)
	}

	for _, bad := range []string{"rp.mk:8080", "https://rp.mk/preview", "ftp://rp.mk", "rp_mk", "-rp.mk"} {
		t.Setenv("ROUNDPEN_PREVIEW_DOMAIN", bad)
		if _, err := config.Load(); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}
