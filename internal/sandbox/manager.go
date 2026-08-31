package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// Service implements Manager.
// BrowserCloser tears down a host-side browser sidecar for a sandbox.
type BrowserCloser interface {
	CloseSandbox(id string)
}

type Service struct {
	store        Store
	backend      backend.Backend
	fs           workspace.FS
	templates    *template.Service
	browser      BrowserCloser
	defaultImage string
	defaultTTL   time.Duration
	logger       *slog.Logger
}

// NewService constructs a sandbox manager.
func NewService(store Store, be backend.Backend, fs workspace.FS, defaultImage string, defaultTTL time.Duration, logger *slog.Logger, opts ...ServiceOption) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{
		store:        store,
		backend:      be,
		fs:           fs,
		defaultImage: defaultImage,
		defaultTTL:   defaultTTL,
		logger:       logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ServiceOption configures sandbox.Service.
type ServiceOption func(*Service)

// WithTemplates attaches the template registry resolver.
func WithTemplates(t *template.Service) ServiceOption {
	return func(s *Service) { s.templates = t }
}

// WithBrowser attaches a host-side browser sidecar closer.
func WithBrowser(b BrowserCloser) ServiceOption {
	return func(s *Service) { s.browser = b }
}

// SetDefaults updates default image and TTL for new sandboxes.
func (s *Service) SetDefaults(image string, ttl time.Duration) {
	if strings.TrimSpace(image) != "" {
		s.defaultImage = strings.TrimSpace(image)
	}
	if ttl > 0 {
		s.defaultTTL = ttl
	}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Sandbox, error) {
	templateRef := req.TemplateID
	image := req.Image
	cpuCount := 1
	memoryMB := 512
	diskSizeMB := 5120
	var templateBuildID, internalTemplateID string
	var useImageCmd bool
	var startCmd string

	if s.templates != nil {
		ref := templateRef
		if ref == "" && image != "" {
			ref = image
		}
		if ref != "" {
			resolved, err := s.templates.Resolve(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("template: %w", err)
			}
			if image == "" {
				image = resolved.Image
			}
			if templateRef == "" {
				templateRef = resolved.Alias
			}
			cpuCount = resolved.CPUCount
			memoryMB = resolved.MemoryMB
			diskSizeMB = resolved.DiskSizeMB
			templateBuildID = resolved.BuildID
			internalTemplateID = resolved.TemplateID
			useImageCmd = resolved.UseImageCmd
			startCmd = resolved.StartCmd
			if req.Metadata == nil {
				req.Metadata = map[string]string{}
			}
			if resolved.Profile != "" {
				req.Metadata["profile"] = resolved.Profile
			}
			if strings.EqualFold(resolved.Profile, "browser") {
				useImageCmd = true
			}
		}
	}
	if image == "" {
		image = templateRef
	}
	if image == "" {
		image = s.defaultImage
	}
	if templateRef == "" {
		templateRef = image
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
	hostPath := info.HostPath
	if abs, err := filepath.Abs(hostPath); err == nil {
		hostPath = abs
	}

	now := time.Now().UTC()
	exp := now.Add(ttl)
	name := NormalizeName(req.Name)
	if name == "" {
		name = defaultName(id)
	}
	category := NormalizeCategory(req.Category)
	isDefault := req.IsDefault && category != ""
	if isDefault {
		if err := s.store.ClearDefaultInCategory(ctx, category, id); err != nil {
			_ = s.fs.Remove(ctx, wsID)
			return nil, err
		}
	}
	sb := &Sandbox{
		ID:            id,
		Name:          name,
		Category:      category,
		IsDefault:     isDefault,
		Status:        StatusCreating,
		Image:         image,
		WorkspaceID:   wsID,
		WorkspacePath: hostPath,
		TTLSeconds:    int(ttl.Seconds()),
		ExpiresAt:     &exp,
		LastActiveAt:  now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Metadata:      req.Metadata,
		CPUCount:      cpuCount,
		MemoryMB:      memoryMB,
		DiskSizeMB:    diskSizeMB,
		TemplateBuild: templateBuildID,
	}
	if sb.Metadata == nil {
		sb.Metadata = map[string]string{}
	}
	if templateRef != "" {
		sb.Metadata["templateID"] = templateRef
	}

	if err := s.store.Insert(ctx, sb); err != nil {
		_ = s.fs.Remove(ctx, wsID)
		if errors.Is(err, ErrConflict) {
			return nil, fmt.Errorf("%w: sandbox name already exists", ErrConflict)
		}
		return nil, fmt.Errorf("store insert: %w", err)
	}

	engineID, err := s.backend.Create(ctx, backend.CreateOpts{
		SandboxID:   id,
		Name:        name,
		Image:       image,
		MountDir:    hostPath,
		Env:         req.Env,
		CPULimit:    float64(cpuCount),
		MemoryLimit: int64(memoryMB) * 1024 * 1024,
		UseImageCmd: useImageCmd,
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

	if startCmd != "" && s.backend.Name() == "kern" {
		// T2 partial: cold-start long-running process on kern (no snapshot support).
		go func() {
			execCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = s.backend.Exec(execCtx, id, backend.ExecOpts{
				Cmd:     []string{"/bin/sh", "-c", startCmd + " >/tmp/roundpen-start.log 2>&1 &"},
				WorkDir: "/workspace",
			})
		}()
	}

	sb.Status = StatusRunning
	sb.UpdatedAt = time.Now().UTC()
	if err := s.store.Update(ctx, sb); err != nil {
		return nil, err
	}
	if s.templates != nil && internalTemplateID != "" {
		s.templates.RecordSpawn(ctx, internalTemplateID)
	}
	s.logger.Info("sandbox created", slog.String("id", id), slog.String("image", image), slog.String("template", templateRef))
	return sb, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Sandbox, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	fillDefaultName(sb)
	return sb, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]*Sandbox, error) {
	var (
		list []*Sandbox
		err  error
	)
	if cat := NormalizeCategory(filter.Category); cat != "" {
		list, err = s.store.ListByCategory(ctx, cat)
	} else {
		list, err = s.store.List(ctx)
	}
	if err != nil {
		return nil, err
	}
	for _, sb := range list {
		fillDefaultName(sb)
	}
	return list, nil
}

func (s *Service) Resolve(ctx context.Context, req ResolveRequest) (*Sandbox, error) {
	if name := NormalizeName(req.Name); name != "" {
		sb, err := s.store.GetByName(ctx, name)
		if err != nil {
			return nil, err
		}
		fillDefaultName(sb)
		return sb, nil
	}
	cat := NormalizeCategory(req.Category)
	if cat == "" {
		return nil, fmt.Errorf("name or category is required")
	}
	sb, err := s.store.GetDefaultByCategory(ctx, cat)
	if err == nil {
		fillDefaultName(sb)
		return sb, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// No explicit default: pick most recently active in the category.
	list, listErr := s.store.ListByCategory(ctx, cat)
	if listErr != nil {
		return nil, listErr
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	fillDefaultName(list[0])
	return list[0], nil
}

func fillDefaultName(sb *Sandbox) {
	if sb != nil && sb.Name == "" {
		sb.Name = defaultName(sb.ID)
	}
}

func (s *Service) Stop(ctx context.Context, id string) error {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.backend.Stop(ctx, id); err != nil {
		s.logger.Warn("backend stop", slog.String("id", id), slog.Any("err", err))
	}
	if s.browser != nil {
		s.browser.CloseSandbox(id)
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
	if s.browser != nil {
		s.browser.CloseSandbox(id)
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

// Connect returns sandbox details, starting the backend when stopped or paused.
// The bool is true when the sandbox was resumed (E2B 201); false when already running (E2B 200).
func (s *Service) Connect(ctx context.Context, id string) (*Sandbox, bool, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	fillDefaultName(sb)

	switch sb.Status {
	case StatusRunning:
		if err := s.hydrate(ctx, sb); err != nil {
			return nil, false, err
		}
		if err := s.Touch(ctx, id); err != nil {
			return nil, false, err
		}
		got, err := s.Get(ctx, id)
		return got, false, err

	case StatusStopped, StatusPaused:
		if err := s.hydrate(ctx, sb); err != nil {
			return nil, false, fmt.Errorf("connect: %w", err)
		}
		if err := s.backend.Start(ctx, id); err != nil {
			return nil, false, fmt.Errorf("connect start: %w", err)
		}
		sb.Status = StatusRunning
		sb.UpdatedAt = time.Now().UTC()
		if err := s.store.Update(ctx, sb); err != nil {
			return nil, false, err
		}
		if err := s.Touch(ctx, id); err != nil {
			return nil, false, err
		}
		got, err := s.Get(ctx, id)
		return got, true, err

	case StatusFailed:
		return nil, false, fmt.Errorf("sandbox %s is %s", id, sb.Status)

	default:
		return nil, false, fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
}

// Refresh extends sandbox TTL from now using the current TTLSeconds (E2B refreshes).
func (s *Service) Refresh(ctx context.Context, id string) (*Sandbox, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	ttl := time.Duration(sb.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	return s.SetTimeout(ctx, id, ttl)
}

func (s *Service) Rename(ctx context.Context, id, name string) (*Sandbox, error) {
	n := name
	return s.Update(ctx, id, UpdateRequest{Name: &n})
}

func (s *Service) Update(ctx context.Context, id string, req UpdateRequest) (*Sandbox, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		n := NormalizeName(*req.Name)
		if n == "" {
			return nil, fmt.Errorf("name is required")
		}
		sb.Name = n
	}
	if req.Category != nil {
		sb.Category = NormalizeCategory(*req.Category)
	}
	if req.IsDefault != nil {
		sb.IsDefault = *req.IsDefault
	}
	if sb.Category == "" {
		sb.IsDefault = false
	}
	if sb.IsDefault {
		if err := s.store.ClearDefaultInCategory(ctx, sb.Category, sb.ID); err != nil {
			return nil, err
		}
	}
	sb.UpdatedAt = time.Now().UTC()
	if err := s.store.Update(ctx, sb); err != nil {
		if errors.Is(err, ErrConflict) {
			return nil, fmt.Errorf("%w: name or category default conflict", ErrConflict)
		}
		return nil, err
	}
	// Refresh in-memory backend metadata (kern hostname) when still running.
	if sb.Status == StatusRunning && sb.WorkspacePath != "" {
		_, _ = s.backend.Create(ctx, backend.CreateOpts{
			SandboxID: sb.ID,
			Name:      sb.Name,
			Image:     sb.Image,
			MountDir:  sb.WorkspacePath,
		})
	}
	return sb, nil
}

// NormalizeName trims and clamps a display name (max 64 runes).
func NormalizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 64 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// NormalizeCategory trims and clamps a category label (max 32 runes).
func NormalizeCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range category {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 32 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func defaultName(id string) string {
	short := strings.ReplaceAll(id, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return "sandbox-" + short
}

func (s *Service) Exec(ctx context.Context, id string, req ExecRequest) (*ExecResult, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sb.Status != StatusRunning {
		return nil, fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
	if err := s.hydrate(ctx, sb); err != nil {
		return nil, err
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
	_ = s.Touch(ctx, id)
	return &ExecResult{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

func (s *Service) workspaceID(ctx context.Context, id string) (string, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if sb.WorkspaceID == "" {
		return "", fmt.Errorf("sandbox %s has no workspace", id)
	}
	return sb.WorkspaceID, nil
}

// WorkspaceHostPath returns the absolute host directory for a sandbox workspace.
func (s *Service) WorkspaceHostPath(ctx context.Context, id string) (string, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return s.mountPath(ctx, sb)
}

func (s *Service) mountPath(ctx context.Context, sb *Sandbox) (string, error) {
	mount := sb.WorkspacePath
	if mount == "" && sb.WorkspaceID != "" {
		info, err := s.fs.Get(ctx, sb.WorkspaceID)
		if err != nil {
			return "", err
		}
		mount = info.HostPath
	}
	if mount == "" {
		return "", fmt.Errorf("sandbox %s has no workspace path", sb.ID)
	}
	if !filepath.IsAbs(mount) {
		if abs, err := filepath.Abs(mount); err == nil {
			mount = abs
		}
	}
	return mount, nil
}

// hydrate re-attaches in-memory backends (kern) after roundpend restart.
func (s *Service) hydrate(ctx context.Context, sb *Sandbox) error {
	if s.backend.Name() != "kern" {
		return nil
	}
	mount, err := s.mountPath(ctx, sb)
	if err != nil {
		return err
	}
	if _, err := s.backend.Create(ctx, backend.CreateOpts{
		SandboxID: sb.ID,
		Name:      sb.Name,
		Image:     sb.Image,
		MountDir:  mount,
	}); err != nil {
		return fmt.Errorf("hydrate: %w", err)
	}
	if sb.Status == StatusRunning {
		if err := s.backend.Start(ctx, sb.ID); err != nil {
			return fmt.Errorf("hydrate start: %w", err)
		}
	}
	return nil
}

func (s *Service) ListFiles(ctx context.Context, id, relPath string) ([]workspace.DirEntry, error) {
	wsID, err := s.workspaceID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.fs.List(ctx, wsID, relPath)
}

func (s *Service) StatFile(ctx context.Context, id, relPath string) (*workspace.FileStat, error) {
	wsID, err := s.workspaceID(ctx, id)
	if err != nil {
		return nil, err
	}
	fi, err := s.fs.Stat(ctx, wsID, relPath)
	if err != nil {
		return nil, err
	}
	return &workspace.FileStat{
		Name:    fi.Name(),
		IsDir:   fi.IsDir(),
		Size:    fi.Size(),
		ModTime: fi.ModTime().UTC(),
	}, nil
}

func (s *Service) ReadFile(ctx context.Context, id, relPath string) (io.ReadCloser, error) {
	wsID, err := s.workspaceID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.fs.Open(ctx, wsID, relPath)
}

func (s *Service) WriteFile(ctx context.Context, id, relPath string, r io.Reader) error {
	wsID, err := s.workspaceID(ctx, id)
	if err != nil {
		return err
	}
	return s.fs.Write(ctx, wsID, relPath, r)
}

func (s *Service) RemoveFile(ctx context.Context, id, relPath string) error {
	wsID, err := s.workspaceID(ctx, id)
	if err != nil {
		return err
	}
	return s.fs.RemovePath(ctx, wsID, relPath)
}

func (s *Service) Dial(ctx context.Context, id string, port int) (net.Conn, error) {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sb.Status != StatusRunning {
		return nil, fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
	if err := s.hydrate(ctx, sb); err != nil {
		return nil, err
	}
	return s.backend.Dial(ctx, id, port)
}

func (s *Service) Touch(ctx context.Context, id string) error {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	sb.LastActiveAt = now
	sb.UpdatedAt = now
	if sb.TTLSeconds > 0 {
		exp := now.Add(time.Duration(sb.TTLSeconds) * time.Second)
		sb.ExpiresAt = &exp
	}
	return s.store.Update(ctx, sb)
}

func (s *Service) AttachTerminal(ctx context.Context, id, sessionKey string, opts TerminalOpts, stdin io.Reader, stdout io.Writer) error {
	sb, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if sb.Status != StatusRunning {
		return fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
	if err := s.hydrate(ctx, sb); err != nil {
		return err
	}
	_ = s.Touch(ctx, id)
	return s.backend.AttachPTY(ctx, id, sessionKey, backend.PTYOpts{
		Cmd: opts.Cmd, WorkDir: opts.WorkDir, Env: opts.Env,
		Rows: opts.Rows, Cols: opts.Cols,
	}, stdin, stdout)
}

func (s *Service) ResizeTerminal(ctx context.Context, id, sessionKey string, rows, cols uint16) error {
	return s.backend.ResizePTY(ctx, id, sessionKey, rows, cols)
}

var _ Manager = (*Service)(nil)
