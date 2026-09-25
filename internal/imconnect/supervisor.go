package imconnect

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chenhg5/cc-connect/core"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Deps are Roundpen services shared by all per-assistant engines.
type Deps struct {
	Assistants  *assistant.Store
	Store       *agentsession.Store
	ACP         *manager.Manager
	Users       storage.UserStore
	Provisioner *agentenv.Provisioner
	LLMGW       *llmgw.Gateway
	LLMEnv      func(userID string) agentenv.LLMEnv // per-user model pins for provisioned sandboxes
	Provider    string                              // default "sysadmin"
	Lang        string                              // "en" | "zh"
	DataDir     string                              // root for per-assistant session maps
}

// Supervisor owns one cc-connect Engine per assistant that has IM channels.
type Supervisor struct {
	deps Deps

	mu      sync.Mutex
	engines map[string]*Engine // assistantID → engine
}

// NewSupervisor builds a supervisor. Call StartAll after HTTP deps are ready.
func NewSupervisor(deps Deps) (*Supervisor, error) {
	if deps.Assistants == nil || deps.Store == nil || deps.ACP == nil {
		return nil, fmt.Errorf("imconnect: assistants, store, and acp are required")
	}
	if deps.Provider == "" {
		deps.Provider = "sysadmin"
	}
	if deps.Lang == "" {
		deps.Lang = "zh"
	}
	if deps.DataDir == "" {
		deps.DataDir = filepath.Join(os.TempDir(), "roundpen-im")
	}
	if err := os.MkdirAll(deps.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("imconnect: data dir: %w", err)
	}
	return &Supervisor{deps: deps, engines: make(map[string]*Engine)}, nil
}

// StartAll loads active assistants and starts engines for those with channels.
func (s *Supervisor) StartAll(ctx context.Context) error {
	list, err := s.deps.Assistants.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, a := range list {
		if err := s.Sync(ctx, a); err != nil {
			slog.Error("imconnect: sync assistant", "assistant", a.ID, "err", err)
		}
	}
	return nil
}

// Sync starts, restarts, or stops the engine for one assistant.
func (s *Supervisor) Sync(ctx context.Context, a *assistant.Assistant) error {
	if a == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.engines[a.ID]; ok {
		_ = existing.Stop()
		delete(s.engines, a.ID)
	}

	if a.Status != assistant.StatusActive || !a.ImChannels.HasEnabledChannel() {
		return nil
	}

	eng, err := s.buildEngine(a)
	if err != nil {
		return err
	}
	if err := eng.Start(); err != nil {
		_ = eng.Stop()
		return err
	}
	s.engines[a.ID] = eng
	slog.Info("imconnect: engine started",
		"assistant", a.ID, "name", a.Name, "channels", channelNames(a.ImChannels))
	return nil
}

// StopAll tears down every engine.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, eng := range s.engines {
		if err := eng.Stop(); err != nil {
			slog.Error("imconnect: stop", "assistant", id, "err", err)
		}
		delete(s.engines, id)
	}
}

func (s *Supervisor) buildEngine(a *assistant.Assistant) (*Engine, error) {
	platforms, err := PlatformsFromChannels(a.ImChannels)
	if err != nil {
		return nil, err
	}
	if len(platforms) == 0 {
		return nil, fmt.Errorf("imconnect: no ready platforms for assistant %s", a.ID)
	}

	role := string(storage.RoleUser)
	apiKey, modelSource := "", ""
	if s.deps.Users != nil {
		if u, err := s.deps.Users.GetByUsername(context.Background(), a.UserID); err == nil && u != nil {
			role = string(u.Role)
			apiKey = u.APIKey
			modelSource = u.ModelSource
		}
	}

	agent := &Agent{
		Store:       s.deps.Store,
		ACP:         s.deps.ACP,
		Provisioner: s.deps.Provisioner,
		LLMGW:       s.deps.LLMGW,
		LLMEnv:      s.deps.LLMEnv,
		ProviderID:  s.deps.Provider,
		UserID:      a.UserID,
		AssistantID: a.ID,
		Role:        role,
		APIKey:      apiKey,
		ModelSource: modelSource,
	}

	dir := filepath.Join(s.deps.DataDir, a.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sessionStore := filepath.Join(dir, "sessions.json")
	lang := core.Language(s.deps.Lang)
	name := "roundpen-" + a.ID
	engine := core.NewEngine(name, agent, platforms, sessionStore, lang)
	return &Engine{assistantID: a.ID, engine: engine, agent: agent}, nil
}

func channelNames(ch assistant.ImChannels) []string {
	var names []string
	for _, typ := range assistant.SupportedChannelTypes() {
		c, ok := ch[typ]
		if ok && c.Enabled && assistant.ChannelReady(typ, c) {
			names = append(names, typ)
		}
	}
	return names
}

// Engine wraps one cc-connect engine for an assistant.
type Engine struct {
	assistantID string
	engine      *core.Engine
	agent       *Agent
}

// Start starts platforms.
func (e *Engine) Start() error {
	return e.engine.Start()
}

// Stop stops platforms and agent sessions.
func (e *Engine) Stop() error {
	var first error
	if e.engine != nil {
		if err := e.engine.Stop(); err != nil {
			first = err
		}
	}
	if e.agent != nil {
		if err := e.agent.Stop(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// EnvDefaults reads process-level IM defaults (lang, data dir, provider).
func EnvDefaults() (lang, dataDir, provider string) {
	lang = strings.TrimSpace(os.Getenv("ROUNDPEN_IM_LANG"))
	dataDir = strings.TrimSpace(os.Getenv("ROUNDPEN_IM_DATA_DIR"))
	provider = strings.TrimSpace(os.Getenv("ROUNDPEN_IM_PROVIDER"))
	if lang == "" {
		lang = "zh"
	}
	if provider == "" {
		provider = "sysadmin"
	}
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "roundpen-im")
	}
	return lang, dataDir, provider
}
