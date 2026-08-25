package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// Service implements Manager.
type Service struct {
	store        Store
	backend      backend.Backend
	fs           workspace.FS
	defaultImage string
	defaultTTL   time.Duration
	logger       *slog.Logger
}

// NewService constructs a sandbox manager.
func NewService(store Store, be backend.Backend, fs workspace.FS, defaultImage string, defaultTTL time.Duration, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:        store,
		backend:      be,
		fs:           fs,
		defaultImage: defaultImage,
		defaultTTL:   defaultTTL,
		logger:       logger,
	}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Sandbox, error) {
	image := req.Image
	if image == "" {
		image = req.TemplateID
	}
	if image == "" {
		image = s.defaultImage
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = s.defaultTTL
	}

	id := uuid.NewString()
	wsID := req.WorkspaceID
	ephemeral := wsID == ""
	if ephemeral {
		wsID = id
	}

	info, err := s.fs.Create(ctx, wsID, ephemeral)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}

	now := time.Now().UTC()
	exp := now.Add(ttl)
	sb := &Sandbox{
		ID:            id,
		Status:        StatusCreating,
		Image:         image,
		WorkspaceID:   wsID,
		WorkspacePath: info.HostPath,
		TTLSeconds:    int(ttl.Seconds()),
		ExpiresAt:     &exp,
		LastActiveAt:  now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Metadata:      req.Metadata,
	}
	if sb.Metadata == nil {
		sb.Metadata = map[string]string{}
	}
	if req.TemplateID != "" {
		sb.Metadata["templateID"] = req.TemplateID
	}

	if err := s.store.Insert(ctx, sb); err != nil {
		_ = s.fs.Remove(ctx, wsID)
		return nil, fmt.Errorf("store insert: %w", err)
	}

	engineID, err := s.backend.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Image:     image,
		MountDir:  info.HostPath,
		Env:       req.Env,
	})
	if err != nil {
		sb.Status = StatusFailed
		sb.UpdatedAt = time.Now().UTC()
		_ = s.store.Update(ctx, sb)
		return nil, fmt.Errorf("backend create: %w", err)
	}
	sb.ContainerID = engineID

	if err := s.backend.Start(ctx, id); err != nil {
		_ = s.backend.Remove(ctx, id)
		sb.Status = StatusFailed
		sb.UpdatedAt = time.Now().UTC()
		_ = s.store.Update(ctx, sb)
		return nil, fmt.Errorf("backend start: %w", err)
	}

	sb.Status = StatusRunning
	sb.UpdatedAt = time.Now().UTC()
	if err := s.store.Update(ctx, sb); err != nil {
		return nil, err
	}
	s.logger.Info("sandbox created", slog.String("id", id), slog.String("image", image))
	return sb, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Sandbox, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*Sandbox, error) {
	return s.store.List(ctx)
}

func (s *Service) Stop(ctx context.Context, id string) error {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.backend.Stop(ctx, id); err != nil {
		s.logger.Warn("backend stop", slog.String("id", id), slog.Any("err", err))
	}
	sb.Status = StatusStopped
	sb.UpdatedAt = time.Now().UTC()
	return s.store.Update(ctx, sb)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.backend.Remove(ctx, id); err != nil {
		s.logger.Warn("backend remove", slog.String("id", id), slog.Any("err", err))
	}
	if sb.WorkspaceID != "" {
		// Only remove ephemeral workspaces (workspace id == sandbox id).
		if sb.WorkspaceID == sb.ID {
			_ = s.fs.Remove(ctx, sb.WorkspaceID)
		}
	}
	return s.store.SoftDelete(ctx, id, time.Now().UTC())
}

func (s *Service) SetTimeout(ctx context.Context, id string, ttl time.Duration) (*Sandbox, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	exp := time.Now().UTC().Add(ttl)
	sb.TTLSeconds = int(ttl.Seconds())
	sb.ExpiresAt = &exp
	sb.LastActiveAt = time.Now().UTC()
	sb.UpdatedAt = sb.LastActiveAt
	if err := s.store.Update(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}

func (s *Service) Exec(ctx context.Context, id string, req ExecRequest) (*ExecResult, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sb.Status != StatusRunning {
		return nil, fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
	res, err := s.backend.Exec(ctx, id, backend.ExecOpts{
		Cmd:     req.Cmd,
		WorkDir: req.WorkDir,
		Env:     req.Env,
		Timeout: req.Timeout,
	})
	if err != nil {
		return nil, err
	}
	sb.LastActiveAt = time.Now().UTC()
	sb.UpdatedAt = sb.LastActiveAt
	_ = s.store.Update(ctx, sb)
	return &ExecResult{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

var _ Manager = (*Service)(nil)
