package template

import (
	"fmt"
	"log/slog"
	"strings"

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
			ExtraArgs:         cfg.KanikoExtraArgs,
		})
		if err != nil {
			return nil, fmt.Errorf("kaniko template builder: %w", err)
		}
		svc.SetBuilder("kaniko", bld)
		logger.Info("template builder enabled",
			slog.String("engine", "kaniko"),
			slog.String("destination", cfg.KanikoDestination),
		)
		return func() { _ = bld.Close() }, nil
	default:
		logger.Info("template builder disabled",
			slog.String("hint", "set ROUNDPEN_TEMPLATE_BUILDER=docker|kaniko or ROUNDPEN_KANIKO_DESTINATION for kern builds"),
		)
		return func() {}, nil
	}
}

// BuilderUnavailableHint returns a user-facing hint when builds are unavailable.
func BuilderUnavailableHint(cfg *config.Config) string {
	switch cfg.ResolveTemplateBuilder() {
	case "kaniko":
		return "template builds use kaniko; ensure executor is on PATH and registry credentials are configured"
	case "docker":
		return "template builds use docker; ensure DOCKER_HOST is reachable"
	default:
		if strings.EqualFold(cfg.Backend, "kern") {
			return "template builds require docker backend or kaniko (set ROUNDPEN_TEMPLATE_BUILDER=kaniko and ROUNDPEN_KANIKO_DESTINATION)"
		}
		return "template builds require docker backend or kaniko builder configuration"
	}
}
