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
			name: "explicit kaniko",
			cfg:  config.Config{TemplateBuilder: "kaniko", KanikoDestination: "reg/t"},
			want: "kaniko",
		},
		{
			name: "auto docker backend",
			cfg:  config.Config{Backend: "docker"},
			want: "docker",
		},
		{
			name: "auto qemu with kaniko destination",
			cfg:  config.Config{Backend: "qemu", KanikoDestination: "reg/t"},
			want: "kaniko",
		},
		{
			name: "kern without kaniko",
			cfg:  config.Config{Backend: "kern"},
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
