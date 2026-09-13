package template

import (
	"fmt"
	"log/slog"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

// AttachBuilder wires a template image builder from process config.
func AttachBuilder(cfg *config.Config, svc *Service, logger *slog.Logger) (func(), error) {
	mode := cfg.ResolveTemplateBuilder()
	switch mode {
	case "docker":
		bld, err := builder.NewDocker(cfg.DockerHost)
		if err != nil {
			return nil, fmt.Errorf("docker template builder: %w", err)
		}
		svc.SetBuilder("docker", bld)
		logger.Info("template builder enabled", slog.String("engine", "docker"))
		return func() { _ = bld.Close() }, nil
	case "ci":
		logger.Info("template builder set to remote CI",
			slog.String("hint", "local StartBuild is disabled; push recipes to CI to produce images"),
		)
		return func() {}, nil
	default:
		logger.Info("template builder disabled",
			slog.String("hint", "set ROUNDPEN_TEMPLATE_BUILDER=docker|ci in Settings when needed"),
		)
		return func() {}, nil
	}
}

// BuilderUnavailableHint returns a user-facing hint when builds are unavailable.
func BuilderUnavailableHint(cfg *config.Config) string {
	switch cfg.ResolveTemplateBuilder() {
	case "docker":
		return "template builds use docker; ensure DOCKER_HOST is reachable"
	case "ci":
		return "template builds are delegated to remote CI; local builds are disabled"
	default:
		return "template builds disabled — choose Docker or remote CI in Settings"
	}
}
