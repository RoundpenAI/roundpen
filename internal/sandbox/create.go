package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/backend"
)

// createSpec is the resolved launch configuration for a new sandbox.
type createSpec struct {
	image              string
	templateRef        string
	ttl                time.Duration
	cpuCount           int
	memoryMB           int
	diskSizeMB         int
	templateBuildID    string
	internalTemplateID string
	useImageCmd        bool
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Sandbox, error) {
	spec, metadata, err := s.resolveCreateSpec(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Metadata = metadata

	id, wsID, ephemeral, hostPath, err := s.createWorkspace(ctx, req)
	if err != nil {
		return nil, err
	}

	sb, err := s.insertSandboxRecord(ctx, req, spec, id, wsID, hostPath, ephemeral)
	if err != nil {
		return nil, err
	}

	if err := s.createEngine(ctx, sb, spec, req.Env, ephemeral); err != nil {
		return nil, err
	}

	sb.Status = StatusRunning
	sb.UpdatedAt = time.Now().UTC()
	if err := s.store.Update(ctx, sb); err != nil {
		return nil, err
	}
	if s.templates != nil && spec.internalTemplateID != "" {
		s.templates.RecordSpawn(ctx, spec.internalTemplateID)
	}
	s.logger.Info("sandbox created", slog.String("id", id), slog.String("image", spec.image), slog.String("template", spec.templateRef))
	audit.Record(ctx, "sandbox.create", "sandbox_id", id, "image", spec.image, "backend", s.backend.Name())
	return sb, nil
}

// resolveCreateSpec applies template resolution and service defaults. It may
// annotate req.Metadata with template profile/slot hints; callers must use the
// returned map because it is replaced when the incoming map was nil.
func (s *Service) resolveCreateSpec(ctx context.Context, req CreateRequest) (createSpec, map[string]string, error) {
	spec := createSpec{
		templateRef: req.TemplateID,
		image:       req.Image,
		cpuCount:    1,
		memoryMB:    512,
		diskSizeMB:  5120,
	}

	if s.templates != nil {
		ref := spec.templateRef
		if ref == "" && spec.image != "" {
			ref = spec.image
		}
		if ref != "" {
			resolved, err := s.templates.Resolve(ctx, ref)
			if err != nil {
				return spec, req.Metadata, fmt.Errorf("template: %w", err)
			}
			if spec.image == "" {
				spec.image = resolved.Image
			}
			if spec.templateRef == "" {
				spec.templateRef = resolved.Alias
			}
			spec.cpuCount = resolved.CPUCount
			spec.memoryMB = resolved.MemoryMB
			spec.diskSizeMB = resolved.DiskSizeMB
			spec.templateBuildID = resolved.BuildID
			spec.internalTemplateID = resolved.TemplateID
			spec.useImageCmd = resolved.UseImageCmd
			if req.Metadata == nil {
				req.Metadata = map[string]string{}
			}
			if resolved.Profile != "" {
				req.Metadata["profile"] = resolved.Profile
			}
			if resolved.Slot != "" {
				req.Metadata["slot"] = resolved.Slot
			}
			if strings.EqualFold(resolved.Slot, "browser") || strings.EqualFold(resolved.Profile, "browser") {
				spec.useImageCmd = true
			}
		}
	}
	if spec.image == "" {
		spec.image = spec.templateRef
	}
	if spec.image == "" {
		spec.image = s.defaultImage
	}
	if spec.templateRef == "" {
		spec.templateRef = spec.image
	}
	spec.ttl = req.TTL
	if spec.ttl <= 0 {
		spec.ttl = s.defaultTTL
	}
	return spec, req.Metadata, nil
}

// createWorkspace allocates the workspace directory and reports the sandbox
// id, workspace id, whether the workspace is ephemeral, and its absolute host
// path.
func (s *Service) createWorkspace(ctx context.Context, req CreateRequest) (id, wsID string, ephemeral bool, hostPath string, err error) {
	id = strings.TrimSpace(req.ID)
	if id == "" {
		id = uuid.NewString()
	}
	wsID = req.WorkspaceID
	ephemeral = wsID == ""
	if ephemeral {
		wsID = id
	}

	info, err := s.fs.Create(ctx, wsID, ephemeral)
	if err != nil {
		return "", "", false, "", fmt.Errorf("workspace: %w", err)
	}
	hostPath = info.HostPath
	if abs, absErr := filepath.Abs(hostPath); absErr == nil {
		hostPath = abs
	}
	return id, wsID, ephemeral, hostPath, nil
}

// insertSandboxRecord clears any earlier default in the target category and
// stores the new sandbox row. An ephemeral workspace is removed when the row
// cannot be written.
func (s *Service) insertSandboxRecord(ctx context.Context, req CreateRequest, spec createSpec, id, wsID, hostPath string, ephemeral bool) (*Sandbox, error) {
	now := time.Now().UTC()
	exp := now.Add(spec.ttl)
	name := NormalizeName(req.Name)
	if name == "" {
		name = defaultName(id)
	}
	category := NormalizeCategory(req.Category)
	isDefault := req.IsDefault && category != ""
	actor, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if isDefault {
		if err := s.store.ClearDefaultInCategory(ctx, category, id, actor.Username); err != nil {
			if ephemeral {
				_ = s.fs.Remove(ctx, wsID)
			}
			return nil, err
		}
	}
	sb := &Sandbox{
		ID:            id,
		Name:          name,
		Category:      category,
		IsDefault:     isDefault,
		Owner:         actor.Username,
		Status:        StatusCreating,
		Image:         spec.image,
		WorkspaceID:   wsID,
		WorkspacePath: hostPath,
		TTLSeconds:    int(spec.ttl.Seconds()),
		ExpiresAt:     &exp,
		LastActiveAt:  now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Metadata:      req.Metadata,
		CPUCount:      spec.cpuCount,
		MemoryMB:      spec.memoryMB,
		DiskSizeMB:    spec.diskSizeMB,
		TemplateBuild: spec.templateBuildID,
	}
	if sb.Metadata == nil {
		sb.Metadata = map[string]string{}
	}
	if spec.templateRef != "" {
		sb.Metadata["templateID"] = spec.templateRef
	}

	if err := s.store.Insert(ctx, sb); err != nil {
		if ephemeral {
			_ = s.fs.Remove(ctx, wsID)
		}
		if errors.Is(err, ErrConflict) {
			return nil, fmt.Errorf("%w: sandbox name already exists", ErrConflict)
		}
		return nil, fmt.Errorf("store insert: %w", err)
	}
	return sb, nil
}

// createEngine launches the backend container and starts it, marking the row
// failed when either step fails.
func (s *Service) createEngine(ctx context.Context, sb *Sandbox, spec createSpec, reqEnv map[string]string, ephemeral bool) error {
	createEnv := sandboxEnv(reqEnv, sb.Metadata["slot"])

	engineUser := ""
	if !ephemeral {
		// Persistent user workspaces must run as their owner: engines drop
		// capabilities, so a default-user root process cannot write a
		// foreign-owned bind mount.
		engineUser = hostPathOwner(sb.WorkspacePath)
	}
	engineID, err := s.backend.Create(ctx, backend.CreateOpts{
		SandboxID:   sb.ID,
		Name:        sb.Name,
		Image:       sb.Image,
		MountDir:    sb.WorkspacePath,
		Env:         createEnv,
		CPULimit:    float64(spec.cpuCount),
		MemoryLimit: int64(spec.memoryMB) * 1024 * 1024,
		UseImageCmd: spec.useImageCmd,
		Slot:        sb.Metadata["slot"],
		Engine:      sb.Metadata["engine"],
		User:        engineUser,
	})
	if err != nil {
		sb.Status = StatusFailed
		sb.UpdatedAt = time.Now().UTC()
		_ = s.store.Update(ctx, sb)
		return fmt.Errorf("backend create: %w", err)
	}
	sb.ContainerID = engineID

	if err := s.backend.Start(ctx, sb.ID); err != nil {
		_ = s.backend.Remove(ctx, sb.ID)
		sb.Status = StatusFailed
		sb.UpdatedAt = time.Now().UTC()
		_ = s.store.Update(ctx, sb)
		return fmt.Errorf("backend start: %w", err)
	}
	return nil
}

// sandboxEnv copies the caller's environment and injects the template slot so
// in-container tooling can detect it.
func sandboxEnv(reqEnv map[string]string, slot string) map[string]string {
	createEnv := reqEnv
	if createEnv == nil {
		createEnv = map[string]string{}
	} else {
		copied := make(map[string]string, len(reqEnv)+1)
		for k, v := range reqEnv {
			copied[k] = v
		}
		createEnv = copied
	}
	if slot != "" {
		createEnv["ROUNDPEN_SLOT"] = slot
	}
	return createEnv
}
