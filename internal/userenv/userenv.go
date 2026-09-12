// Package userenv maps each user to fixed environment slots (agent / browser / mobile).
package userenv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

const (
	SlotAgent   = "agent"
	SlotBrowser = "browser"
	SlotMobile  = "mobile"
)

// Store persists user → environment mappings.
type Store struct {
	DB *sql.DB
}

// Mapping is one fixed slot binding.
type Mapping struct {
	UserID     string
	Slot       string
	SandboxID  string
	TemplateID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (s *Store) Get(ctx context.Context, userID, slot string) (*Mapping, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT user_id, slot, sandbox_id, template_id, created_at, updated_at
		FROM user_environments WHERE user_id=$1 AND slot=$2`, userID, slot)
	var m Mapping
	err := row.Scan(&m.UserID, &m.Slot, &m.SandboxID, &m.TemplateID, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) List(ctx context.Context, userID string) ([]Mapping, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT user_id, slot, sandbox_id, template_id, created_at, updated_at
		FROM user_environments WHERE user_id=$1 ORDER BY slot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mapping
	for rows.Next() {
		var m Mapping
		if err := rows.Scan(&m.UserID, &m.Slot, &m.SandboxID, &m.TemplateID, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Upsert(ctx context.Context, userID, slot, sandboxID, templateID string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_environments (user_id, slot, sandbox_id, template_id, updated_at)
		VALUES ($1,$2,$3,$4,now())
		ON CONFLICT (user_id, slot) DO UPDATE SET
			sandbox_id=EXCLUDED.sandbox_id,
			template_id=EXCLUDED.template_id,
			updated_at=now()`,
		userID, slot, sandboxID, templateID)
	return err
}

func (s *Store) Delete(ctx context.Context, userID, slot string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_environments WHERE user_id=$1 AND slot=$2`, userID, slot)
	return err
}

// slotStore persists user → environment mappings.
type slotStore interface {
	Get(ctx context.Context, userID, slot string) (*Mapping, error)
	List(ctx context.Context, userID string) ([]Mapping, error)
	Upsert(ctx context.Context, userID, slot, sandboxID, templateID string) error
	Delete(ctx context.Context, userID, slot string) error
}

var errSlotFailed = errors.New("sandbox is failed")

// Config controls Ensure* defaults.
type Config struct {
	BrowserTemplate string // default "browser-desktop"
	AgentTemplate   string // default "code-agent"
	PublicURL       string // control-plane / llmgw base, e.g. http://127.0.0.1:9527
	VirtualKey      string // llmgw virtual key (not an upstream key)
	DefaultModel    func() string
}

// SetGateway records the public base URL and virtual key injected into new slots.
func (s *Service) SetGateway(publicURL, virtualKey string) {
	if s == nil {
		return
	}
	s.Config.PublicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if virtualKey != "" {
		s.Config.VirtualKey = virtualKey
	}
}

func (s *Service) gatewayEnv() map[string]string {
	base := strings.TrimRight(s.Config.PublicURL, "/")
	if base == "" {
		return nil
	}
	key := strings.TrimSpace(s.Config.VirtualKey)
	if key == "" {
		key = "vk-roundpen-internal"
	}
	env := map[string]string{
		"ROUNDPEN_URL":         base,
		"OPENAI_BASE_URL":      base + "/llmgw/openai",
		"ANTHROPIC_BASE_URL":   base + "/llmgw/anthropic",
		"OPENAI_API_KEY":       key,
		"ANTHROPIC_API_KEY":    key,
		"ANTHROPIC_AUTH_TOKEN": key,
	}
	if s.Config.DefaultModel != nil {
		agentenv.ApplyDefaultModel(env, s.Config.DefaultModel())
	}
	return env
}

// Service ensures fixed environments for a user.
type Service struct {
	Store     slotStore
	Sandboxes sandbox.Manager
	Git       *gitcred.Store
	Probe     *runtime.Probe
	Config    Config
}

func (s *Service) browserTemplate() string {
	t := strings.TrimSpace(s.Config.BrowserTemplate)
	if t == "" {
		return "browser-desktop"
	}
	return t
}

// EnsureBrowser starts or resumes the user's Browser QEMU environment.
func (s *Service) EnsureBrowser(ctx context.Context, userID string) (*sandbox.Sandbox, error) {
	if s.Probe != nil {
		if err := s.Probe.RequireBrowser(); err != nil {
			return nil, err
		}
	}
	return s.ensure(ctx, userID, SlotBrowser, s.browserTemplate(), "Browser", runtime.EngineQEMU)
}

// EnsureAgent starts or resumes the user's Cloud Agent environment.
// Agent is always Docker + code-agent (or Config.AgentTemplate override).
func (s *Service) EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error) {
	engine := runtime.EngineDocker
	if s.Probe != nil {
		if err := s.Probe.RequireAgent(engine); err != nil {
			return nil, err
		}
	}
	templateID := "code-agent"
	if t := strings.TrimSpace(s.Config.AgentTemplate); t != "" {
		templateID = t
	}
	sb, err := s.ensure(ctx, userID, SlotAgent, templateID, "Agent", engine)
	if err != nil {
		return nil, err
	}
	s.injectGit(ctx, userID, sb.ID)
	return sb, nil
}

// EnvView is the API shape for one slot.
type EnvView struct {
	Slot       string `json:"slot"`
	SandboxID  string `json:"sandboxId,omitempty"`
	TemplateID string `json:"templateId,omitempty"`
	Status     string `json:"status"`
	Name       string `json:"name,omitempty"`
}

// List returns agent/browser(+mobile placeholder) views for the user.
func (s *Service) List(ctx context.Context, userID string) ([]EnvView, error) {
	slots := []string{SlotAgent, SlotBrowser, SlotMobile}
	maps, err := s.Store.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	bySlot := map[string]Mapping{}
	for _, m := range maps {
		bySlot[m.Slot] = m
	}
	out := make([]EnvView, 0, len(slots))
	for _, slot := range slots {
		v := EnvView{Slot: slot, Status: "absent"}
		if slot == SlotMobile {
			v.Status = "reserved"
			out = append(out, v)
			continue
		}
		if m, ok := bySlot[slot]; ok {
			v.SandboxID = m.SandboxID
			v.TemplateID = m.TemplateID
			sb, err := s.Sandboxes.Get(ctx, m.SandboxID)
			if err != nil {
				v.Status = "missing"
			} else {
				v.Status = string(sb.Status)
				v.Name = sb.Name
			}
		}
		if (v.Status == "absent" || v.Status == "missing") && s.Sandboxes != nil {
			if sb, err := s.Sandboxes.Resolve(ctx, sandbox.ResolveRequest{Name: slotSandboxName(slot, userID)}); err == nil && sb != nil {
				v.SandboxID = sb.ID
				v.Status = string(sb.Status)
				v.Name = sb.Name
				if v.TemplateID == "" && sb.Metadata != nil {
					v.TemplateID = sb.Metadata["templateID"]
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) injectGit(ctx context.Context, userID, sandboxID string) {
	if s == nil || s.Sandboxes == nil || s.Git == nil {
		return
	}
	script, err := s.Git.GuestInstallScript(ctx, userID)
	if err != nil {
		slog.Warn("git credentials script", "user", userID, "err", err)
		return
	}
	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := s.Sandboxes.Exec(execCtx, sandboxID, sandbox.ExecRequest{
		Cmd:     []string{"/bin/sh", "-c", script},
		WorkDir: "/workspace",
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		slog.Warn("inject git credentials", "user", userID, "sandbox", sandboxID, "err", err)
		return
	}
	if res != nil && res.ExitCode != 0 {
		slog.Warn("inject git credentials", "user", userID, "sandbox", sandboxID, "exit", res.ExitCode, "stderr", string(res.Stderr))
	}
}

// BrowserSandboxID returns the mapped browser env id if present.
func (s *Service) BrowserSandboxID(ctx context.Context, userID string) (string, error) {
	m, err := s.Store.Get(ctx, userID, SlotBrowser)
	if err != nil || m == nil {
		return "", err
	}
	return m.SandboxID, nil
}

func (s *Service) ensure(ctx context.Context, userID, slot, templateID, category, engine string) (*sandbox.Sandbox, error) {
	if s.Sandboxes == nil {
		return nil, fmt.Errorf("sandboxes not configured")
	}
	name := slotSandboxName(slot, userID)

	if m, err := s.Store.Get(ctx, userID, slot); err != nil {
		return nil, err
	} else if m != nil && m.SandboxID != "" {
		sb, err := s.adopt(ctx, userID, slot, templateID, engine, m.SandboxID)
		if err == nil {
			return sb, nil
		}
		if !errors.Is(err, sandbox.ErrNotFound) && !errors.Is(err, errSlotFailed) {
			return nil, err
		}
		if errors.Is(err, errSlotFailed) {
			_ = s.Sandboxes.Delete(ctx, m.SandboxID)
		}
		_ = s.Store.Delete(ctx, userID, slot)
	}

	if existing, err := s.Sandboxes.Resolve(ctx, sandbox.ResolveRequest{Name: name}); err == nil && existing != nil {
		sb, err := s.adopt(ctx, userID, slot, templateID, engine, existing.ID)
		if err == nil {
			return sb, nil
		}
		if !errors.Is(err, errSlotFailed) {
			return nil, err
		}
		_ = s.Sandboxes.Delete(ctx, existing.ID)
	}

	sb, err := s.createSlot(ctx, userID, slot, templateID, category, name, engine)
	if err == nil {
		if err := s.Store.Upsert(ctx, userID, slot, sb.ID, templateID); err != nil {
			return sb, err
		}
		return sb, nil
	}
	if !errors.Is(err, sandbox.ErrConflict) {
		return nil, err
	}
	existing, rerr := s.Sandboxes.Resolve(ctx, sandbox.ResolveRequest{Name: name})
	if rerr != nil {
		return nil, err
	}
	return s.adopt(ctx, userID, slot, templateID, engine, existing.ID)
}

func (s *Service) adopt(ctx context.Context, userID, slot, templateID, engine, sandboxID string) (*sandbox.Sandbox, error) {
	sb, err := s.Sandboxes.Get(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	switch sb.Status {
	case sandbox.StatusRunning, sandbox.StatusCreating:
	case sandbox.StatusPaused, sandbox.StatusStopped:
		if _, _, err := s.Sandboxes.Connect(ctx, sandboxID); err != nil {
			return nil, err
		}
		sb, err = s.Sandboxes.Get(ctx, sandboxID)
		if err != nil {
			return nil, err
		}
	case sandbox.StatusFailed:
		return nil, errSlotFailed
	default:
		return nil, fmt.Errorf("sandbox %s is %s", sandboxID, sb.Status)
	}
	if engine != "" && strings.TrimSpace(sb.Image) != "" && runtime.EngineOfImage(sb.Image) != engine {
		return nil, errSlotFailed
	}
	if err := s.Store.Upsert(ctx, userID, slot, sb.ID, templateID); err != nil {
		return sb, err
	}
	return sb, nil
}

func (s *Service) createSlot(ctx context.Context, userID, slot, templateID, category, name, engine string) (*sandbox.Sandbox, error) {
	env := map[string]string{
		"ROUNDPEN_SLOT":    slot,
		"ROUNDPEN_USER_ID": userID,
	}
	for k, v := range s.gatewayEnv() {
		env[k] = v
	}
	meta := map[string]string{
		"slot":   slot,
		"userId": userID,
	}
	if engine != "" {
		meta["engine"] = engine
	}
	create := sandbox.CreateRequest{
		TemplateID: templateID,
		Name:       name,
		Category:   category,
		IsDefault:  true,
		Env:        env,
		Metadata:   meta,
		TTL:        24 * time.Hour,
	}
	if slot == SlotAgent {
		create.ID = workspace.AgentSandboxID(userID)
		create.WorkspaceID = workspace.UserWorkspaceID(userID)
	}
	return s.Sandboxes.Create(ctx, create)
}

func slotSandboxName(slot, userID string) string {
	return fmt.Sprintf("%s-%s", slot, sanitizeUser(userID))
}

func sanitizeUser(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	var b strings.Builder
	for _, r := range u {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 24 {
		out = out[:24]
	}
	if out == "" {
		return "user"
	}
	return out
}
