package template

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

// SetLogger configures structured logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// SetBuilder attaches an image builder (nil when template builds are disabled).
func (s *Service) SetBuilder(backend string, b builder.Runner) {
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

// CreateBuild allocates a new waiting build under an existing template (new version).
func (s *Service) CreateBuild(ctx context.Context, templateID string, req CreateBuildRequest) (CreateBuildResult, error) {
	out, err := s.store.CreateBuild(ctx, templateID, req)
	if err != nil {
		return CreateBuildResult{}, err
	}
	assign := true
	if req.AssignDefault != nil {
		assign = *req.AssignDefault
	}
	s.assignDefaultMu.Lock()
	s.assignDefault[out.BuildID] = assign
	s.assignDefaultMu.Unlock()
	return out, nil
}

// StartBuild launches an async template build (T1/T2).
// When the target build is ready and the core spec (cache key) changed, a new
// build ID is allocated automatically so prior versions stay intact.
func (s *Service) StartBuild(ctx context.Context, templateID, buildID string, spec BuildSpec, opts CreateBuildRequest) (StartBuildResult, error) {
	if s.builder == nil {
		return StartBuildResult{}, fmt.Errorf("template builds are not configured (set ROUNDPEN_TEMPLATE_BUILDER=docker|kaniko)")
	}
	info, err := s.store.GetBuild(ctx, templateID, buildID)
	if err != nil {
		return StartBuildResult{}, err
	}
	if info.Status != BuildWaiting && info.Status != BuildError && info.Status != BuildReady {
		return StartBuildResult{}, fmt.Errorf("build status %q cannot be started", info.Status)
	}

	cacheKey, err := builder.CacheKey(spec)
	if err != nil {
		return StartBuildResult{}, err
	}

	forked := false
	if info.Status == BuildReady && info.CacheKey != "" && info.CacheKey != cacheKey {
		cpu, mem, disk := opts.CPUCount, opts.MemoryMB, opts.DiskSizeMB
		if cpu <= 0 {
			cpu = spec.CPUCount
		}
		if mem <= 0 {
			mem = spec.MemoryMB
		}
		created, err := s.CreateBuild(ctx, templateID, CreateBuildRequest{
			Tags:          opts.Tags,
			AssignDefault: opts.AssignDefault,
			CPUCount:      cpu,
			MemoryMB:      mem,
			DiskSizeMB:    disk,
		})
		if err != nil {
			return StartBuildResult{}, err
		}
		s.logBuild(created.BuildID, "info", "version", "core spec changed; forked new build from "+buildID)
		buildID = created.BuildID
		forked = true
	} else if len(opts.Tags) > 0 {
		if err := s.store.UpsertTags(ctx, templateID, buildID, opts.Tags); err != nil {
			return StartBuildResult{}, err
		}
	}

	if !forked && opts.AssignDefault != nil {
		s.assignDefaultMu.Lock()
		s.assignDefault[buildID] = *opts.AssignDefault
		s.assignDefaultMu.Unlock()
	}
	assignDefault := s.takeAssignDefault(buildID)

	if !spec.Force {
		if cached, err := s.store.FindCachedBuild(ctx, templateID, cacheKey); err == nil && cached.BuildID != buildID {
			s.logBuild(buildID, "info", "cache", "reusing cached build "+cached.BuildID)
			if err := s.store.FinishBuild(ctx, templateID, buildID, cached.ArtifactRef, cacheKey, cached.Snapshot, assignDefault); err != nil {
				return StartBuildResult{}, err
			}
			s.logBuild(buildID, "info", "cache", "build ready from cache")
			return StartBuildResult{TemplateID: templateID, BuildID: buildID, Forked: forked}, nil
		}
	}

	if info.Status == BuildError || (info.Status == BuildReady && !forked) {
		if err := s.store.ClearBuildLogs(ctx, buildID); err != nil {
			return StartBuildResult{}, err
		}
	}

	if err := s.store.SaveBuildSpec(ctx, buildID, spec, cacheKey); err != nil {
		return StartBuildResult{}, err
	}

	s.buildMu.Lock()
	if _, ok := s.builds[buildID]; ok {
		s.buildMu.Unlock()
		return StartBuildResult{}, fmt.Errorf("build already running")
	}
	s.builds[buildID] = struct{}{}
	s.buildMu.Unlock()

	go s.runBuild(templateID, buildID, spec, cacheKey, assignDefault)
	return StartBuildResult{TemplateID: templateID, BuildID: buildID, Forked: forked}, nil
}

func (s *Service) takeAssignDefault(buildID string) bool {
	s.assignDefaultMu.Lock()
	defer s.assignDefaultMu.Unlock()
	if v, ok := s.assignDefault[buildID]; ok {
		delete(s.assignDefault, buildID)
		return v
	}
	return true
}

func (s *Service) runBuild(templateID, buildID string, spec BuildSpec, cacheKey string, assignDefault bool) {
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

	tplName := "template"
	keepImageCmd := spec.KeepImageCmd
	if rec, err := s.store.GetByID(ctx, templateID); err == nil && rec.Name != "" {
		tplName = rec.Name
		if strings.EqualFold(rec.Profile, "browser") && spec.StartCmd == "" {
			keepImageCmd = true
		}
	}
	spec.KeepImageCmd = keepImageCmd
	var versionTags []string
	if tagged, err := s.store.ListTagsForBuild(ctx, templateID, buildID); err == nil {
		for _, t := range tagged {
			if t != DefaultTag && t != "latest" {
				versionTags = append(versionTags, t)
			}
		}
	}
	tags := builder.TemplateImageTags(tplName, buildID, versionTags...)
	artifact, snapshot, err := s.builder.Build(ctx, baseImage, spec, tags, logFn)
	if err != nil {
		_ = s.store.FailBuild(ctx, buildID, err.Error())
		logFn("error", "build", err.Error())
		return
	}
	if err := s.store.FinishBuild(ctx, templateID, buildID, artifact, cacheKey, snapshot, assignDefault); err != nil {
		logFn("error", "store", err.Error())
		return
	}
	logFn("info", "done", "build ready: "+artifact)
	for _, t := range tags[1:] {
		logFn("info", "done", "also tagged: "+t)
	}
}

// GetBuildStatus returns build status and logs.
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
