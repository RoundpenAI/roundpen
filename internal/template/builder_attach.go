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
	case "kaniko":
		bld, err := builder.NewKaniko(builder.KanikoConfig{
			Executor:          cfg.KanikoExecutor,
			DestinationPrefix: cfg.KanikoDestination,
			Insecure:          cfg.KanikoInsecure,
			SkipTLSVerify:     cfg.KanikoSkipTLSVerify,
			RegistryMirrors:   cfg.KanikoRegistryMirrors,
			ExtraArgs:         cfg.KanikoExtraArgs,
		})
		if err != nil {
			logger.Warn("kaniko template builder unavailable; continuing without local builds",
				slog.Any("err", err),
				slog.String("hint", "install executor (./scripts/install-kaniko.sh) or set Template build engine to Disabled / remote CI in Settings"),
			)
			return func() {}, nil
		}
		svc.SetBuilder("kaniko", bld)
		logger.Info("template builder enabled",
			slog.String("engine", "kaniko"),
			slog.String("destination", cfg.KanikoDestination),
		)
		return func() { _ = bld.Close() }, nil
	case "ci":
		logger.Info("template builder set to remote CI",
			slog.String("hint", "local StartBuild is disabled; push recipes to CI to produce images"),
		)
		return func() {}, nil
	default:
		logger.Info("template builder disabled",
			slog.String("hint", "set ROUNDPEN_TEMPLATE_BUILDER=docker|kaniko|ci in Settings when needed"),
		)
		return func() {}, nil
	}
}

// BuilderUnavailableHint returns a user-facing hint when builds are unavailable.
func BuilderUnavailableHint(cfg *config.Config) string {
	switch cfg.ResolveTemplateBuilder() {
	case "kaniko":
		return "template builds use kaniko; ensure executor + bubblewrap (bwrap) are installed and registry credentials are configured"
	case "docker":
		return "template builds use docker; ensure DOCKER_HOST is reachable"
	case "ci":
		return "template builds are delegated to remote CI; local builds are disabled"
	default:
		return "template builds disabled — choose Docker, local Kaniko, or remote CI in Settings"
	}
}
