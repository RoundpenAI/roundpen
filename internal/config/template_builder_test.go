package config_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestResolveTemplateBuilder(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{
			name: "explicit docker",
			cfg:  config.Config{TemplateBuilder: "docker"},
			want: "docker",
		},
		{
			name: "legacy kaniko maps to docker",
			cfg:  config.Config{TemplateBuilder: "kaniko"},
			want: "docker",
		},
		{
			name: "explicit ci",
			cfg:  config.Config{TemplateBuilder: "ci"},
			want: "ci",
		},
		{
			name: "disabled",
			cfg:  config.Config{TemplateBuilder: "disabled"},
			want: "",
		},
		{
			name: "empty disabled",
			cfg:  config.Config{Backend: "qemu"},
			want: "",
		},
		{
			name: "auto docker backend",
			cfg:  config.Config{TemplateBuilder: "auto", Backend: "docker"},
			want: "docker",
		},
		{
			name: "auto non-docker backend",
			cfg:  config.Config{TemplateBuilder: "auto", Backend: "qemu"},
			want: "",
		},
		{
			name: "docker without builder",
			cfg:  config.Config{Backend: "docker"},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.ResolveTemplateBuilder(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
