package config_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestResolveCDPProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		cfg    *config.Config
		chrome bool
		want   string
	}{
		{
			name:   "explicit host",
			cfg:    &config.Config{Backend: "docker", CDP: config.CDPConfig{Provider: "host"}},
			chrome: false,
			want:   config.CDPProviderHost,
		},
		{
			name:   "auto docker backend",
			cfg:    &config.Config{Backend: "docker", CDP: config.CDPConfig{Provider: "auto"}},
			chrome: true,
			want:   config.CDPProviderDocker,
		},
		{
			name:   "auto docker with chrome stays docker",
			cfg:    &config.Config{Backend: "docker", CDP: config.CDPConfig{Provider: "auto"}},
			chrome: true,
			want:   config.CDPProviderDocker,
		},
		{
			name:   "auto docker without chrome (NAS)",
			cfg:    &config.Config{Backend: "docker", CDP: config.CDPConfig{Provider: "auto"}},
			chrome: false,
			want:   config.CDPProviderDocker,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := config.ResolveCDPProvider(tc.cfg, tc.chrome); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeCDPRemoteRequiresEndpoint(t *testing.T) {
	t.Parallel()
	cfg := config.CDPConfig{Provider: "remote"}
	if err := config.NormalizeCDP(&cfg); err == nil {
		t.Fatal("expected endpoint required")
	}
	cfg.Endpoint = "http://127.0.0.1:9222"
	if err := config.NormalizeCDP(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != config.DefaultCDPPort {
		t.Fatalf("port default: %d", cfg.Port)
	}
}
