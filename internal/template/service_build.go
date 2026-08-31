package template

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

// SetLogger configures structured logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// SetBuilder attaches a Docker builder (nil on kern-only deployments).
func (s *Service) SetBuilder(backend string, b *builder.Docker) {
	s.backend = backend
	s.builder = b
}

// BuildsSupported reports whether template builds can run.
func (s *Service) BuildsSupported() bool {
	return s.builder != nil
}

// CreateTemplate registers a new template with a waiting build.
func (s *Service) CreateTemplate(ctx context.Context, req CreateTemplateRequest) (CreateTemplateResult, error) {
	ref := ParseRef(req.Name)
	if ref.Name != "" {
		req.Name = ref.Name
	}
	if ref.Namespace != "" && ref.Namespace != DefaultNamespace {
		req.Namespace = ref.Namespace
	}
	if ref.Tag != "" && ref.Tag != DefaultTag {
		if req.Tag == "" {
			req.Tag = ref.Tag
		}
	}
	return s.store.CreateTemplate(ctx, req)
}

// StartBuild launches an async template build (T1/T2).
func (s *Service) StartBuild(ctx context.Context, templateID, buildID string, spec BuildSpec) error {
	if s.builder == nil {
		return fmt.Errorf("template builds require docker backend")
	}
	info, err := s.store.GetBuild(ctx, templateID, buildID)
	if err != nil {
		return err
	}
	if info.Status != BuildWaiting && info.Status != BuildError {
		return fmt.Errorf("build status %q cannot be started", info.Status)
	}

	cacheKey, err := builder.CacheKey(spec)
	if err != nil {
		return err
	}
	if !spec.Force {
		if cached, err := s.store.FindCachedBuild(ctx, templateID, cacheKey); err == nil {
			s.logBuild(buildID, "info", "cache", "reusing cached build "+cached.BuildID)
			if err := s.store.FinishBuild(ctx, templateID, buildID, cached.ArtifactRef, cacheKey, cached.Snapshot, true); err != nil {
				return err
			}
			s.logBuild(buildID, "info", "cache", "build ready from cache")
			return nil
		}
	}

	if err := s.store.SaveBuildSpec(ctx, buildID, spec, cacheKey); err != nil {
		return err
	}

	s.buildMu.Lock()
	if _, ok := s.builds[buildID]; ok {
		s.buildMu.Unlock()
		return fmt.Errorf("build already running")
	}
	s.builds[buildID] = struct{}{}
	s.buildMu.Unlock()

	go s.runBuild(templateID, buildID, spec, cacheKey)
	return nil
}

func (s *Service) runBuild(templateID, buildID string, spec BuildSpec, cacheKey string) {
	defer func() {
		s.buildMu.Lock()
		delete(s.builds, buildID)
		s.buildMu.Unlock()
	}()

	ctx := context.Background()
	logFn := func(level, step, msg string) {
		_ = s.store.AppendBuildLog(ctx, buildID, level, step, msg)
		s.logBuild(buildID, level, step, msg)
	}

	baseImage := spec.FromImage
	if baseImage == "" && spec.FromTemplate != "" {
		resolved, err := s.Resolve(ctx, spec.FromTemplate)
		if err != nil {
			_ = s.store.FailBuild(ctx, buildID, err.Error())
			return
		}
		baseImage = resolved.Image
	}
	if baseImage == "" {
		baseImage = "ubuntu:22.04"
	}

	tag := fmt.Sprintf("roundpen/template-%s:latest", buildID)
	artifact, snapshot, err := s.builder.Build(ctx, baseImage, spec, tag, logFn)
	if err != nil {
		_ = s.store.FailBuild(ctx, buildID, err.Error())
		logFn("error", "build", err.Error())
		return
	}
	if err := s.store.FinishBuild(ctx, templateID, buildID, artifact, cacheKey, snapshot, true); err != nil {
		logFn("error", "store", err.Error())
		return
	}
	logFn("info", "done", "build ready: "+artifact)
}

// GetBuildStatus returns build status and logs (E2B-compatible).
func (s *Service) GetBuildStatus(ctx context.Context, templateID, buildID string, logsOffset, limit int) (BuildInfo, []LogEntry, error) {
	info, err := s.store.GetBuild(ctx, templateID, buildID)
	if err != nil {
		return BuildInfo{}, nil, err
	}
	logs, err := s.store.ListBuildLogs(ctx, buildID, logsOffset, limit)
	if err != nil {
		return info, nil, err
	}
	return info, logs, nil
}

func (s *Service) logBuild(buildID, level, step, msg string) {
	s.logger.Info("template build",
		slog.String("build_id", buildID),
		slog.String("level", level),
		slog.String("step", step),
		slog.String("msg", msg),
	)
}
