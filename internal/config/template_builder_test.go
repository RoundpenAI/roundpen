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
			name: "explicit ci",
			cfg:  config.Config{TemplateBuilder: "ci"},
			want: "ci",
		},
		{
			name: "disabled",
			cfg:  config.Config{TemplateBuilder: "disabled", KanikoDestination: "reg/t"},
			want: "",
		},
		{
			name: "empty ignores destination",
			cfg:  config.Config{Backend: "qemu", KanikoDestination: "reg/t"},
			want: "",
		},
		{
			name: "auto docker backend",
			cfg:  config.Config{TemplateBuilder: "auto", Backend: "docker"},
			want: "docker",
		},
		{
			name: "auto qemu with kaniko destination",
			cfg:  config.Config{TemplateBuilder: "auto", Backend: "qemu", KanikoDestination: "reg/t"},
			want: "kaniko",
		},
		{
			name: "docker without kaniko",
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
