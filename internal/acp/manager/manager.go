// Package manager owns ACP connections bound to Roundpen agent sessions.
package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/policy"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/search"
	"github.com/RoundpenAI/roundpen/internal/toolgw"
)

const maxConcurrentPrompts = 3

// ErrSteerUnsupported reports that a provider cannot inject mid-turn messages;
// callers fall back to queueing.
var ErrSteerUnsupported = errors.New("steer unsupported by provider")

// Actor is the user identity for System Agent tools.
type Actor = tools.Actor

// SysDeps are control-plane dependencies for the System Agent.
type SysDeps struct {
	LoopbackBase string
	LLMKey       string
	// LLM resolves the System Agent's provider, model and key for a user.
	LLM          func(userID string) sysagent.LLMConfig
	BrowserHub   *browser.Hub
	BrowserSlots tools.BrowserSlot
	AgentSlots   tools.AgentSlot
	// AgentJobSlots is the dedicated long-job Agent environment (routines /
	// project workers). When nil, AgentSlots is used for those sessions too.
	AgentJobSlots tools.AgentSlot
	// OnRoutineTokens persists per-completion token usage for a run key.
	OnRoutineTokens func(runKey string, delta int)

	// Roundpen is in-process management tools (envs/templates/sessions/settings).
	Roundpen *tools.RoundpenBinder

	// PreviewZone is the wildcard domain previews are served under
	// (ROUNDPEN_PREVIEW_DOMAIN); empty leaves the preview-domain tool out.
	PreviewZone string

	// WebSearch 解析某用户的搜索后端（endpoint/key/proxy）。nil，或 endpoint
	// 与 key 均为空 → 不注册 WebSearch；proxy 同时用于 WebFetch/Skill install
	// 的出口。
	WebSearch func(userID string) search.Config

	// HostFiles exposes granted host directories (NAS shares) to the file tools.
	// nil disables host reads. The returned value is bound to one session.
	HostFiles func(sessionID string) tools.HostFiles

	History sysagent.MessageSource

	// AutoMode classifies tool calls when the chat permission mode is
	// "decide" (nil falls through to asking the user).
	AutoMode automode.Evaluator

	// Google is the per-user Gmail/Calendar connector (nil skips those tools).
	Google tools.GoogleAPI

	// MCP is the per-user MCP connector layer (nil skips MCP tools). Tool
	// snapshots are read once per session start.
	MCP tools.MCPAPI

	// PolicyFor returns the assistant constitution for toolgw hard checks.
	// nil keeps NAS zero-config (only L3 HardDeny inside the gateway).
	PolicyFor func(ctx context.Context, userID, assistantID string) *policy.Profile

	// UserContext returns a system-prompt block describing the human
	// (preferred name, org, timezone, about). Empty string skips injection.
	UserContext func(ctx context.Context, userID string) string
}

// Runtime is a live ACP connection for one agent session.
type Runtime struct {
	SessionID string
	SandboxID string
	Provider  providers.Provider
	// Profile is the session persona used at Start (coordinator / worker role / "").
	Profile string
	// Cwd is the guest absolute working directory used at Start (empty = /workspace).
	Cwd string
	// RunKey is the standing-routine firing bound at Start (empty for chat).
	RunKey string

	bridge *acpclient.Bridge
	conn   *acp.ClientSideConnection
	acpSID acp.SessionId

	// seedHistory: stdio/Claude just did NewSession; first Prompt gets a
	// Postgres transcript preamble (same projection as System Agent).
	seedHistory bool

	// setSysMode sets how the in-process System Agent approves tool calls;
	// nil for external (stdio) runtimes.
	setSysMode func(string)

	// steerFunc injects a mid-turn user message into the running turn; nil for
	// providers without mid-turn injection (external agents).
	steerFunc func(string) error

	// rebindRoutine swaps FinishRun's run key and token budget on a live
	// System Agent so continue segments keep the same ACP session.
	rebindRoutine func(runKey, autonomy string, tokenBudget int)

	gen    uint64
	cancel context.CancelFunc
	mu     sync.Mutex
}

// CanRebindRoutine reports whether continue segments can reuse this runtime.
func (rt *Runtime) CanRebindRoutine() bool {
	return rt != nil && rt.rebindRoutine != nil
}

// RebindRoutine updates the live run tools and token ceiling for a new segment.
func (rt *Runtime) RebindRoutine(runKey, autonomy string, tokenBudget int) {
	if rt == nil || rt.rebindRoutine == nil {
		return
	}
	rt.rebindRoutine(runKey, autonomy, tokenBudget)
	rt.mu.Lock()
	rt.RunKey = runKey
	rt.mu.Unlock()
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
	nextGen  atomic.Uint64
}

// New creates a Manager.
func New(log *slog.Logger, sandboxes sandbox.Manager, list []providers.Provider, sys SysDeps) *Manager {
	if log == nil {
		log = slog.Default()
	}
	if list == nil {
		list = providers.Default()
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

// RoutineStart marks a session as one firing of a standing routine.
// An empty RunKey is an ordinary chat.
type RoutineStart struct {
	RunKey      string
	Autonomy    string // read | browse
	TokenBudget int    // remaining chain budget for this segment; 0 = unlimited
}

// ProjectStart selects a project coordinator / code-dev worker persona.
// Empty Profile keeps the default System Agent surface.
type ProjectStart struct {
	Profile string // coordinator | implementer | reviewer | tester | ""
	// Cwd is the guest absolute working directory (e.g. /workspace/projects/foo).
	// Empty keeps the default /workspace root.
	Cwd string
	// Key / Name / RepoPath bind the session to a Project (coordinator + workers).
	Key      string // PRJ-n
	Name     string
	RepoPath string // workspace-relative, e.g. projects/foo
}

// StartOpts configures Start.
type StartOpts struct {
	Actor       Actor
	AssistantID string
	Routine     RoutineStart
	Project     ProjectStart
}

func (opts StartOpts) useJobSlot() bool {
	if opts.Routine.RunKey != "" {
		return true
	}
	switch opts.Project.Profile {
	case "implementer", "reviewer", "tester":
		return true
	}
	return false
}

// mcpBinder loads the user's MCP tool snapshots for one session. Scheduled
// runs skip MCP entirely (no approval surface). Snapshot failures degrade to
// no MCP tools instead of blocking the session.
func (m *Manager) mcpBinder(userID string, routine bool) *tools.MCPBinder {
	if m.sys.MCP == nil || routine {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	servers, err := m.sys.MCP.ToolsForSession(ctx, userID)
	if err != nil {
		m.log.Warn("mcp tool snapshot", slog.String("user", userID), slog.Any("err", err))
		return nil
	}
	if len(servers) == 0 {
		return nil
	}
	return &tools.MCPBinder{API: m.sys.MCP, Servers: servers}
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

	bridge := acpclient.New(m.log, m.sandboxes, sandboxID, opts.Actor.Authz())
	// AttachExec runs through the sandbox manager's authorize(), so the run
	// context must carry the same actor the session was started with.
	runCtx, cancel := context.WithCancel(authz.WithActor(context.Background(), opts.Actor.Authz()))

	var conn *acp.ClientSideConnection
	var runtimeGen uint64
	var sysAgent *sysagent.Agent
	var reg *tools.Registry
	var controlPlane *tools.RoundpenHTTP
	runKeyHold := &atomic.Value{}
	runKeyHold.Store(opts.Routine.RunKey)
	switch p.Mode {
	case "sysadmin", "mock", "":
		c2aR, c2aW := io.Pipe()
		a2cR, a2cW := io.Pipe()
		llmCfg := sysagent.LLMConfig{APIKey: m.sys.LLMKey}
		if m.sys.LLM != nil {
			llmCfg = m.sys.LLM(opts.Actor.Username)
			if llmCfg.APIKey == "" {
				llmCfg.APIKey = m.sys.LLMKey
			}
		}
		reg = tools.NewRegistry()
		controlPlane = &tools.RoundpenHTTP{BaseURL: m.sys.LoopbackBase}
		agentSlots := m.sys.AgentSlots
		if opts.useJobSlot() && m.sys.AgentJobSlots != nil {
			agentSlots = m.sys.AgentJobSlots
		}
		binder := &tools.AgentBinder{
			Slots: agentSlots,
			Exec:  m.sandboxes,
			Files: m.sandboxes,
			Cwd:   strings.TrimSpace(opts.Project.Cwd),
		}
		if m.sys.HostFiles != nil {
			binder.Host = m.sys.HostFiles(sessionID)
		}
		searchCfg := search.Config{}
		if m.sys.WebSearch != nil {
			searchCfg = m.sys.WebSearch(opts.Actor.Username)
		}
		webClient := tools.NewWebHTTPClient(tools.WebClientOptions{
			// NAS / home-lab: LAN hosts are a primary workplace. Still blocks
			// loopback and link-local (cloud metadata) via the dial guard.
			AllowPrivate: true,
			ProxyURL:     searchCfg.ProxyURL,
		})
		tools.RegisterSession(reg, tools.SessionSurface{
			Roundpen:    m.sys.Roundpen,
			API:         controlPlane,
			SessionID:   sessionID,
			RunKey:      opts.Routine.RunKey,
			Autonomy:    opts.Routine.Autonomy,
			Profile:     opts.Project.Profile,
			ProjectKey:  strings.TrimSpace(opts.Project.Key),
			Browser:     &tools.BrowserBinder{Hub: m.sys.BrowserHub, Slots: m.sys.BrowserSlots, SessionID: sessionID},
			Agent:       binder,
			PreviewZone: m.sys.PreviewZone,
			Web:         &tools.WebBinder{HTTP: webClient, Model: llmCfg},
			WebHTTP:     webClient,
			Search: &tools.WebSearchBinder{
				Endpoint: searchCfg.Endpoint,
				APIKey:   searchCfg.APIKey,
				HTTP:     webClient,
				Vendor:   searchCfg.Vendor,
				BaseURL:  searchCfg.BaseURL,
				Model:    searchCfg.Model,
			},
			Google: &tools.GoogleBinder{API: m.sys.Google},
			MCP:    m.mcpBinder(opts.Actor.Username, opts.Routine.RunKey != ""),
		})
		userID := opts.Actor.Username
		assistantID := opts.AssistantID
		var policyLookup func(context.Context) *policy.Profile
		if m.sys.PolicyFor != nil && assistantID != "" {
			policyLookup = func(c context.Context) *policy.Profile {
				return m.sys.PolicyFor(c, userID, assistantID)
			}
		}
		var onTokens func(int)
		if opts.Routine.RunKey != "" && m.sys.OnRoutineTokens != nil {
			onTokens = func(delta int) {
				key, _ := runKeyHold.Load().(string)
				if key != "" && delta > 0 {
					m.sys.OnRoutineTokens(key, delta)
				}
			}
		}
		var userCtx string
		if m.sys.UserContext != nil {
			userCtx = m.sys.UserContext(ctx, opts.Actor.Username)
		}
		agent := sysagent.New(sysagent.Deps{
			LLM:          llmCfg,
			Tools:        reg,
			Gateway:      toolgw.New(reg),
			PolicyLookup: policyLookup,
			Actor:        opts.Actor,
			History:      m.sys.History,
			SessionID:    sessionID,
			AssistantID:  opts.AssistantID,
			Evaluator:    m.sys.AutoMode,
			Profile:      opts.Project.Profile,
			Cwd:          strings.TrimSpace(opts.Project.Cwd),
			ProjectKey:   strings.TrimSpace(opts.Project.Key),
			ProjectName:  strings.TrimSpace(opts.Project.Name),
			RepoPath:     strings.TrimSpace(opts.Project.RepoPath),
			UserContext:  userCtx,
			TokenBudget:  opts.Routine.TokenBudget,
			OnTokens:     onTokens,
		})
		sysAgent = agent
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
		runtimeGen = m.nextGen.Add(1)
		go func(gen uint64) {
			err := m.sandboxes.AttachExec(runCtx, sandboxID, sandbox.AttachExecOpts{
				Cmd: cmd, WorkDir: "/workspace",
			}, stdinR, stdoutW, io.Discard)
			_ = stdinR.Close()
			_ = stdoutW.Close()
			if err != nil && runCtx.Err() == nil {
				m.log.Warn("attach exec ended", slog.String("session", sessionID), slog.Any("err", err))
			}
			// Drop stale runtime so the next WS can Start fresh instead of
			// accepting prompts on a dead ACP pipe (UI looks connected, chat hangs).
			m.mu.Lock()
			if cur := m.runtimes[sessionID]; cur != nil && cur.gen == gen {
				delete(m.runtimes, sessionID)
			}
			m.mu.Unlock()
			cancel()
		}(runtimeGen)
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
	if projectCwd := strings.TrimSpace(opts.Project.Cwd); projectCwd != "" {
		cwd = projectCwd
	}
	if p.Mode == "sysadmin" || p.Mode == "mock" || p.Mode == "" {
		// In-process sysagent treats "." as host cwd; project Cwd still wins above.
		if strings.TrimSpace(opts.Project.Cwd) == "" {
			cwd = "."
		}
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
		Profile:     opts.Project.Profile,
		Cwd:         strings.TrimSpace(opts.Project.Cwd),
		RunKey:      opts.Routine.RunKey,
		bridge:      bridge,
		conn:        conn,
		acpSID:      sess.SessionId,
		seedHistory: p.Mode == "stdio",
		gen:         runtimeGen,
		cancel:      cancel,
	}
	if sysAgent != nil {
		sid := string(sess.SessionId)
		rt.setSysMode = func(mode string) { sysAgent.SetPermMode(sid, mode) }
		rt.steerFunc = func(text string) error { return sysAgent.SteerSession(sid, text) }
		if opts.Routine.RunKey != "" {
			rt.rebindRoutine = func(runKey, autonomy string, tokenBudget int) {
				_ = autonomy // browse tools stay as registered for the routine
				if runKey != "" {
					tools.RegisterRunTools(reg, controlPlane, sessionID, runKey)
					runKeyHold.Store(runKey)
				}
				sysAgent.ResetTokenAccounting(tokenBudget)
				// Clear leftover cancel so the next Prompt can start cleanly.
				_, _ = sysAgent.ResumeSession(context.Background(), acp.ResumeSessionRequest{
					SessionId: acp.SessionId(sid),
				})
			}
		}
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

// SetPermMode selects how tool calls are approved. The in-process System
// Agent applies it itself; external agents are handled by the runner.
func (rt *Runtime) SetPermMode(mode string) {
	if rt.setSysMode != nil {
		rt.setSysMode(mode)
	}
}

// SetAutoMode keeps the old on/off switch: on is the classifier, off allows
// workspace edits and asks about every other tool.
func (rt *Runtime) SetAutoMode(v bool) {
	mode := automode.ModeEdit
	if v {
		mode = automode.ModeDecide
	}
	rt.SetPermMode(mode)
}

// SteerCapable reports whether the runtime can inject mid-turn messages.
// Nil-safe so callers can query a runner that has no runtime yet.
func (rt *Runtime) SteerCapable() bool { return rt != nil && rt.steerFunc != nil }

// Steer injects a user message into the running turn. The error is either
// ErrSteerUnsupported or a transient condition (no active turn, inbox full);
// callers queue the message in both cases.
func (rt *Runtime) Steer(text string) error {
	if rt == nil || rt.steerFunc == nil {
		return ErrSteerUnsupported
	}
	return rt.steerFunc(text)
}

// Steer is the manager-level entry point for mid-turn injection. It bypasses
// the prompt gate: steering is not a new turn.
func (m *Manager) Steer(_ context.Context, sessionID, text string) error {
	rt, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("runtime not found")
	}
	return rt.Steer(text)
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
