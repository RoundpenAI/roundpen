// Package settings persists mutable Roundpen configuration in PostgreSQL.
package settings

import (
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/template"
)

const globalID = "global"

// AppSettings are admin-editable values stored in PostgreSQL.
type AppSettings struct {
	AllowPublicRegistration bool   `json:"allowPublicRegistration"`
	DefaultImage            string `json:"defaultImage"`
	DefaultTtlSeconds       int    `json:"defaultTtlSeconds"`
	PreviewPublicURL        string `json:"previewPublicUrl"`
	PreviewTokenTtlSeconds  int    `json:"previewTokenTtlSeconds"`
	TemplateBuilder         string `json:"templateBuilder"`
	KanikoDestination       string `json:"kanikoDestination"`
	KanikoInsecure          bool   `json:"kanikoInsecure"`
	KanikoSkipTLSVerify     bool   `json:"kanikoSkipTlsVerify"`
	KanikoExtraArgs         string `json:"kanikoExtraArgs"`
}

// SystemInfo is read-only infrastructure metadata for the settings UI.
type SystemInfo struct {
	Backend               string `json:"backend"`
	DockerHost            string `json:"dockerHost"`
	DataRoot              string `json:"dataRoot"`
	HTTPAddr              string `json:"httpAddr"`
	TemplateBuilderActive string `json:"templateBuilderActive"`
	TemplateBuilderHint   string `json:"templateBuilderHint,omitempty"`
}

// FromConfig extracts DB-backed settings from process config.
func FromConfig(cfg *config.Config) AppSettings {
	return AppSettings{
		AllowPublicRegistration: cfg.AllowPublicRegistration,
		DefaultImage:            cfg.DefaultImage,
		DefaultTtlSeconds:       int(cfg.DefaultTTL / time.Second),
		PreviewPublicURL:        cfg.PreviewPublicURL,
		PreviewTokenTtlSeconds:  int(cfg.PreviewTokenTTL / time.Second),
		TemplateBuilder:         cfg.TemplateBuilder,
		KanikoDestination:       cfg.KanikoDestination,
		KanikoInsecure:          cfg.KanikoInsecure,
		KanikoSkipTLSVerify:     cfg.KanikoSkipTLSVerify,
		KanikoExtraArgs:         strings.Join(cfg.KanikoExtraArgs, " "),
	}
}

// ApplyToConfig writes settings into the in-memory process config.
func ApplyToConfig(s *AppSettings, cfg *config.Config) {
	cfg.AllowPublicRegistration = s.AllowPublicRegistration
	cfg.DefaultImage = strings.TrimSpace(s.DefaultImage)
	cfg.DefaultTTL = time.Duration(s.DefaultTtlSeconds) * time.Second
	cfg.PreviewPublicURL = strings.TrimSpace(s.PreviewPublicURL)
	cfg.PreviewTokenTTL = time.Duration(s.PreviewTokenTtlSeconds) * time.Second
	cfg.TemplateBuilder = strings.ToLower(strings.TrimSpace(s.TemplateBuilder))
	cfg.KanikoDestination = strings.TrimSpace(s.KanikoDestination)
	cfg.KanikoInsecure = s.KanikoInsecure
	cfg.KanikoSkipTLSVerify = s.KanikoSkipTLSVerify
	args := strings.TrimSpace(s.KanikoExtraArgs)
	if args == "" {
		cfg.KanikoExtraArgs = nil
	} else {
		cfg.KanikoExtraArgs = strings.Fields(args)
	}
}

// Validate checks user-editable settings.
func (s AppSettings) Validate() error {
	if strings.TrimSpace(s.DefaultImage) == "" {
		return fmt.Errorf("defaultImage is required")
	}
	if s.DefaultTtlSeconds <= 0 {
		return fmt.Errorf("defaultTtlSeconds must be positive")
	}
	if s.PreviewTokenTtlSeconds <= 0 {
		return fmt.Errorf("previewTokenTtlSeconds must be positive")
	}
	switch strings.ToLower(strings.TrimSpace(s.TemplateBuilder)) {
	case "", "auto", "docker", "kaniko":
	default:
		return fmt.Errorf("templateBuilder must be auto, docker, kaniko, or empty")
	}
	return nil
}

// SystemFromConfig returns read-only system metadata.
func SystemFromConfig(cfg *config.Config) SystemInfo {
	active := cfg.ResolveTemplateBuilder()
	info := SystemInfo{
		Backend:               cfg.Backend,
		DockerHost:            cfg.DockerHost,
		DataRoot:              cfg.EffectiveDataRoot(),
		HTTPAddr:              cfg.HTTPAddr,
		TemplateBuilderActive: active,
	}
	if active == "" {
		info.TemplateBuilderHint = template.BuilderUnavailableHint(cfg)
	}
	return info
}
