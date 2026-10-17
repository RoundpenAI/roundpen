package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend/docker"
	"github.com/RoundpenAI/roundpen/internal/backend/multi"
	"github.com/RoundpenAI/roundpen/internal/config"
)

func dockerMirrorsFromConfig(cfg *config.Config) (custom []string, disableBuiltin bool) {
	if cfg == nil {
		return nil, false
	}
	return docker.ParseImageMirrors(cfg.ImageMirrors)
}

// preloadDockerImages warms Agent/Browser OCI images in the background so the
// first browser_navigate (or Agent Bash) after a fresh install does not pay a
// multi-GB pull inside a short tool call.
func preloadDockerImages(eng *multi.Backend, cfg *config.Config, logger *slog.Logger) {
	if eng == nil || cfg == nil || !eng.HasDocker() {
		return
	}
	refs := uniqueImageRefs(
		cfg.BrowserImage,
		cfg.AgentImage,
		cfg.DefaultImage,
	)
	if len(refs) == 0 {
		return
	}
	go func() {
		for _, ref := range refs {
			logger.Info("preloading docker image", slog.String("ref", ref))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			err := eng.EnsureLocalImage(ctx, ref)
			cancel()
			if err != nil {
				logger.Warn("preload docker image failed", slog.String("ref", ref), slog.Any("err", err))
				continue
			}
			logger.Info("preload docker image ready", slog.String("ref", ref))
		}
	}()
}

func uniqueImageRefs(refs ...string) []string {
	seen := make(map[string]struct{}, len(refs))
	var out []string
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}
