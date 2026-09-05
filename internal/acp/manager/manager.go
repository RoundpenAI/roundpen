// Package manager owns ACP connections bound to Roundpen agent sessions.
package manager

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const maxConcurrentPrompts = 3

// Actor is the user identity for System Agent tools.
type Actor = tools.Actor

// SysDeps are control-plane dependencies for the System Agent.
type SysDeps struct {
	LoopbackBase string
	LLMKey       string
	DefaultModel func() string
	BrowserHub   *browser.Hub
	History      sysagent.MessageSource
}

// Runtime is a live ACP connection for one agent session.
type Runtime struct {
	SessionID string
	SandboxID string
	Provider  providers.Provider

	bridge *acpclient.Bridge
	conn   *acp.ClientSideConnection
	acpSID acp.SessionId

	// seedHistory: stdio/Claude just did NewSession; first Prompt gets a
	// Postgres transcript preamble (same projection as System Agent).
	seedHistory bool

	cancel context.CancelFunc
	mu     sync.Mutex
}

// Manager tracks runtimes and a global prompt concurrency gate.
type Manager struct {
	log       *slog.Logger
	sandboxes sandbox.Manager
	providers []providers.Provider
	sys       SysDeps

	mu       sync.Mutex
	runtimes map[string]*Runtime
	gate     chan struct{}
}

// New creates a Manager.
func New(log *slog.Logger, sandboxes sandbox.Manager, list []providers.Provider, sys SysDeps) *Manager {
	if log == nil {
		log = slog.Default()
	}
	if list == nil {
		list = providers.Default()
	}
	if sys.LLMKey == "" {
		sys.LLMKey = llmgw.InternalVirtualKey
	}
	return &Manager{
		log:       log,
		sandboxes: sandboxes,
		providers: list,
		sys:       sys,
		runtimes:  make(map[string]*Runtime),
		gate:      make(chan struct{}, maxConcurrentPrompts),
	}
}

// Providers returns configured providers.
func (m *Manager) Providers() []providers.Provider {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]providers.Provider, len(m.providers))
	copy(out, m.providers)
	return out
}

// StartOpts configures Start.
type StartOpts struct {
	AutoApprove bool
	Actor       Actor
}

// Start connects an ACP agent for the given Roundpen agent session.
func (m *Manager) Start(ctx context.Context, sessionID, sandboxID string, providerID string, opts StartOpts) (*Runtime, error) {
	p, ok := providers.ByID(m.providers, providerID)
	if !ok || !p.Enabled {
		return nil, fmt.Errorf("provider %q not found or disabled", providerID)
	}

	m.mu.Lock()
	if _, exists := m.runtimes[sessionID]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s already has a runtime", sessionID)
	}
	m.mu.Unlock()

	bridge := acpclient.New(m.log, m.sandboxes, sandboxID, opts.AutoApprove)
	runCtx, cancel := context.WithCancel(context.Background())

	var conn *acp.ClientSideConnection
	switch p.Mode {
	case "sysadmin", "mock", "":
		c2aR, c2aW := io.Pipe()
		a2cR, a2cW := io.Pipe()
		reg := tools.NewRegistry()
		tools.RegisterRoundpen(reg, &tools.RoundpenHTTP{BaseURL: m.sys.LoopbackBase})
		tools.RegisterBrowser(reg, &tools.BrowserBinder{Hub: m.sys.BrowserHub, SessionID: sessionID})
		agent := sysagent.New(sysagent.Deps{
			LLM: sysagent.LLMConfig{
				BaseURL:      strings.TrimRight(m.sys.LoopbackBase, "/") + "/llmgw/openai",
				APIKey:       m.sys.LLMKey,
				DefaultModel: m.sys.DefaultModel,
			},
			Tools:     reg,
			Actor:     opts.Actor,
			History:   m.sys.History,
			SessionID: sessionID,
		})
		asc := acp.NewAgentSideConnection(agent, a2cW, c2aR)
		agent.SetAgentConnection(asc)
		asc.SetLogger(m.log)
		conn = acp.NewClientSideConnection(bridge, c2aW, a2cR)
		conn.SetLogger(m.log)
		go func() {
			<-runCtx.Done()
			_ = c2aW.Close()
			_ = a2cW.Close()
		}()
	case "stdio":
		cmd := []string{p.Command}
		cmd = append(cmd, p.Args...)
		stdinR, stdinW := io.Pipe()
		stdoutR, stdoutW := io.Pipe()
		go func() {
			err := m.sandboxes.AttachExec(runCtx, sandboxID, sandbox.AttachExecOpts{
				Cmd: cmd, WorkDir: "/workspace",
			}, stdinR, stdoutW, io.Discard)
			_ = stdinR.Close()
			_ = stdoutW.Close()
			if err != nil && runCtx.Err() == nil {
				m.log.Warn("attach exec ended", slog.String("session", sessionID), slog.Any("err", err))
			}
		}()
		conn = acp.NewClientSideConnection(bridge, stdinW, stdoutR)
		conn.SetLogger(m.log)
		go func() {
			<-runCtx.Done()
			_ = stdinW.Close()
		}()
	default:
		cancel()
		return nil, fmt.Errorf("unknown provider mode %q", p.Mode)
	}

	initWait := 30 * time.Second
	if p.Mode == "stdio" {
		initWait = 2 * time.Minute // QEMU boot + ACP adapter
	}
	initCtx, initCancel := context.WithTimeout(ctx, initWait)
	defer initCancel()
	if _, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		},
	}); err != nil {
		cancel()
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	cwd := "/workspace"
	if p.Mode == "sysadmin" || p.Mode == "mock" || p.Mode == "" {
		cwd = "."
	}
	sess, err := conn.NewSession(initCtx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp new session: %w", err)
	}

	rt := &Runtime{
		SessionID:   sessionID,
		SandboxID:   sandboxID,
		Provider:    p,
		bridge:      bridge,
		conn:        conn,
		acpSID:      sess.SessionId,
		seedHistory: p.Mode == "stdio",
		cancel:      cancel,
	}
	m.mu.Lock()
	m.runtimes[sessionID] = rt
	m.mu.Unlock()
	return rt, nil
}

// Get returns a runtime by Roundpen agent session id.
func (m *Manager) Get(sessionID string) (*Runtime, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.runtimes[sessionID]
	return rt, ok
}

// Stop tears down a runtime.
func (m *Manager) Stop(sessionID string) {
	m.mu.Lock()
	rt := m.runtimes[sessionID]
	delete(m.runtimes, sessionID)
	m.mu.Unlock()
	if rt != nil && rt.cancel != nil {
		rt.cancel()
	}
}

// SetEventHandler wires UI events for a runtime.
func (rt *Runtime) SetEventHandler(fn func(acpclient.Event)) {
	rt.bridge.SetOnEvent(fn)
}

// SetPermissionHandler wires permission prompts.
func (rt *Runtime) SetPermissionHandler(fn func(acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)) {
	rt.bridge.SetPermissionHandler(fn)
}

// SetAutoApprove toggles ordinary permission auto-approval on the live bridge.
func (rt *Runtime) SetAutoApprove(v bool) {
	if rt.bridge != nil {
		rt.bridge.SetAutoApprove(v)
	}
}

// Prompt sends a user message (respects global concurrency gate).
func (m *Manager) Prompt(ctx context.Context, sessionID, text string) (acp.StopReason, error) {
	rt, ok := m.Get(sessionID)
	if !ok {
		return "", fmt.Errorf("runtime not found")
	}
	select {
	case m.gate <- struct{}{}:
		defer func() { <-m.gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	blocks := []acp.ContentBlock{acp.TextBlock(text)}
	rt.mu.Lock()
	seed := rt.seedHistory
	rt.seedHistory = false
	rt.mu.Unlock()
	if seed {
		if pre := m.restorePreamble(ctx, sessionID, text); pre != "" {
			blocks = []acp.ContentBlock{acp.TextBlock(pre), acp.TextBlock(text)}
		}
	}
	resp, err := rt.conn.Prompt(ctx, acp.PromptRequest{
		SessionId: rt.acpSID,
		Prompt:    blocks,
	})
	if err != nil {
		return "", err
	}
	return resp.StopReason, nil
}

// Cancel cancels the current turn.
func (m *Manager) Cancel(ctx context.Context, sessionID string) error {
	rt, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("runtime not found")
	}
	return rt.conn.Cancel(ctx, acp.CancelNotification{SessionId: rt.acpSID})
}

func (m *Manager) restorePreamble(ctx context.Context, sessionID, current string) string {
	if m.sys.History == nil || sessionID == "" {
		return ""
	}
	rows, err := m.sys.History.ListMessages(ctx, sessionID, 2000)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return sysagent.RestorePreamble(rows, current)
}
