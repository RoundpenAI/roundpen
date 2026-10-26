// Package expertrun runs one-shot coding ACP specialists (Claude Code) on the
// user's Agent Docker slot without registering a Manager runtime on the parent
// sysadmin session.
package expertrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusNeedsUser = "needs_user"
	StatusNeedsAuth = "needs_auth"

	defaultTimeout = 45 * time.Minute
	initTimeout    = 2 * time.Minute
)

// ErrNeedsAuth signals specialist vendor login is required.
var ErrNeedsAuth = errors.New("needs_auth")

// Request is one one-shot specialist run.
type Request struct {
	UserID           string
	Actor            authz.Actor
	ExpertID         string
	Command          string
	Args             []string
	Goal             string
	Paths            []string
	RepoPath         string
	Branch           string
	IssueURL         string
	PRURL            string
	ChangeSetSummary string
	Constraints      string
	Cwd              string // default /workspace
	Timeout          time.Duration
}

// Result is the structured handoff back to System Agent.
type Result struct {
	Status       string
	Summary      string
	ChangedPaths []string
	DiffStat     map[string]int
	PRIntent     map[string]any
	NotesForUser string
	ExpertID     string
	StopReason   string
	RawReply     string
	DeepLink     string
	Duration     time.Duration
	Retried      bool
}

// AgentSlot starts the user's interactive Agent environment.
type AgentSlot interface {
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
}

// SessionDriver runs one Prompt after AttachExec. Nil → real ACP over Sandboxes.
type SessionDriver interface {
	RunOnce(ctx context.Context, sandboxID, cwd string, cmd []string, prompt string, actor authz.Actor) (reply, stopReason string, err error)
}

// Runner executes Claude (etc.) one-shot on the Agent slot.
type Runner struct {
	Log       *slog.Logger
	Slots     AgentSlot
	Sandboxes sandbox.Manager
	Driver    SessionDriver // nil = defaultACPDriver
	DeepLink  string        // default settings deep link for needs_auth

	mu   sync.Mutex
	busy map[string]bool // userID → in-flight
}

// New returns a Runner.
func New(log *slog.Logger, slots AgentSlot, sandboxes sandbox.Manager) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{
		Log:       log,
		Slots:     slots,
		Sandboxes: sandboxes,
		DeepLink:  "/settings?section=git",
		busy:      make(map[string]bool),
	}
}

// Busy reports whether a user already has an expert run in flight.
func (r *Runner) Busy(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy[strings.TrimSpace(userID)]
}

func (r *Runner) setBusy(userID string, v bool) error {
	userID = strings.TrimSpace(userID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if v {
		if r.busy[userID] {
			return fmt.Errorf("expert already running for this user")
		}
		r.busy[userID] = true
		return nil
	}
	delete(r.busy, userID)
	return nil
}

// ProbeAuth ensures the Agent slot and tries ACP Initialize for command.
func (r *Runner) ProbeAuth(ctx context.Context, userID string, actor authz.Actor, expertID, command string) error {
	if r == nil || r.Slots == nil || r.Sandboxes == nil {
		return fmt.Errorf("expert runner not configured")
	}
	userID = strings.TrimSpace(userID)
	command = strings.TrimSpace(command)
	if userID == "" || command == "" {
		return fmt.Errorf("user and command required")
	}
	sb, err := r.Slots.EnsureAgent(ctx, userID)
	if err != nil {
		return fmt.Errorf("agent workspace: %w", err)
	}
	driver := r.driver()
	_, _, err = driver.RunOnce(ctx, sb.ID, "/workspace", []string{command}, "", actor)
	if err != nil {
		if looksLikeAuthErr(err) {
			return fmt.Errorf("%w: %v", ErrNeedsAuth, err)
		}
		return err
	}
	return nil
}

func (r *Runner) driver() SessionDriver {
	if r.Driver != nil {
		return r.Driver
	}
	return &defaultACPDriver{Log: r.Log, Sandboxes: r.Sandboxes}
}

// Run executes one specialist turn (with a single retry on transient failure).
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	out := Result{
		ExpertID: req.ExpertID,
		PRIntent: map[string]any{"action": "none"},
		DeepLink: r.DeepLink,
	}
	if r == nil || r.Slots == nil || r.Sandboxes == nil {
		out.Status = StatusFailed
		out.Summary = "expert runner not configured"
		return out, fmt.Errorf("expert runner not configured")
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = strings.TrimSpace(req.Actor.Username)
	}
	if userID == "" {
		out.Status = StatusFailed
		out.Summary = "username required"
		return out, fmt.Errorf("username required")
	}
	if strings.TrimSpace(req.Goal) == "" {
		out.Status = StatusFailed
		out.Summary = "goal required"
		return out, fmt.Errorf("goal required")
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		out.Status = StatusFailed
		out.Summary = "command required"
		return out, fmt.Errorf("command required")
	}
	if err := r.setBusy(userID, true); err != nil {
		out.Status = StatusFailed
		out.Summary = err.Error()
		out.NotesForUser = "Wait for the current specialist run to finish."
		return out, err
	}
	defer r.setBusy(userID, false)

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sb, err := r.Slots.EnsureAgent(runCtx, userID)
	if err != nil {
		out.Status = StatusFailed
		out.Summary = "agent workspace unavailable"
		out.Duration = time.Since(start)
		return out, err
	}
	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		cwd = "/workspace"
	}
	argv := append([]string{cmd}, req.Args...)
	brief := BuildBrief(req)
	driver := r.driver()

	reply, stop, err := driver.RunOnce(runCtx, sb.ID, cwd, argv, brief, req.Actor)
	if err != nil && isTransient(err) && runCtx.Err() == nil {
		out.Retried = true
		r.Log.Info("expert retry", "expert", req.ExpertID, "err", err)
		reply, stop, err = driver.RunOnce(runCtx, sb.ID, cwd, argv, brief, req.Actor)
	}
	out.StopReason = stop
	out.RawReply = reply
	out.Duration = time.Since(start)

	if err != nil {
		if looksLikeAuthErr(err) {
			out.Status = StatusNeedsAuth
			out.Summary = "specialist requires login"
			out.NotesForUser = "Open " + out.DeepLink + " (or assistant Experts tab), complete Claude login / Git, then retry DelegateToExpert."
			return out, fmt.Errorf("%w: %v", ErrNeedsAuth, err)
		}
		out.Status = StatusFailed
		out.Summary = err.Error()
		return out, err
	}

	parsed := ParseContract(reply)
	out.Status = parsed.Status
	out.Summary = parsed.Summary
	out.ChangedPaths = parsed.ChangedPaths
	out.PRIntent = parsed.PRIntent
	out.NotesForUser = parsed.NotesForUser
	if out.PRIntent == nil {
		out.PRIntent = map[string]any{"action": "none"}
	}

	switch strings.ToLower(strings.TrimSpace(stop)) {
	case "", string(acp.StopReasonEndTurn):
		// ok
	case string(acp.StopReasonRefusal):
		if out.Status == StatusSucceeded {
			out.Status = StatusFailed
		}
		if out.NotesForUser == "" {
			out.NotesForUser = "specialist refused the request"
		}
	case string(acp.StopReasonCancelled):
		out.Status = StatusFailed
		if out.Summary == "" {
			out.Summary = "specialist cancelled"
		}
	}

	paths, stat := r.gitChanged(runCtx, sb.ID, cwd)
	if len(out.ChangedPaths) == 0 && len(paths) > 0 {
		out.ChangedPaths = paths
	}
	if len(stat) > 0 {
		out.DiffStat = stat
	}
	if out.ChangedPaths == nil {
		out.ChangedPaths = []string{}
	}
	return out, nil
}

func (r *Runner) gitChanged(ctx context.Context, sandboxID, cwd string) ([]string, map[string]int) {
	if r.Sandboxes == nil {
		return nil, nil
	}
	res, err := r.Sandboxes.Exec(ctx, sandboxID, sandbox.ExecRequest{
		Cmd:     []string{"/bin/sh", "-c", "git status --porcelain 2>/dev/null | awk '{print $NF}'"},
		WorkDir: cwd,
		Timeout: 30 * time.Second,
	})
	if err != nil || res == nil {
		return nil, nil
	}
	var paths []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, map[string]int{"files": len(paths)}
}

func isTransient(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{"connection reset", "broken pipe", "eof", "i/o timeout", "temporary", "attach exec"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func looksLikeAuthErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNeedsAuth) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"not logged", "login required", "unauthorized", "authentication",
		"auth required", "please run", "claude auth", "api key", "anthropic",
		"needs_auth", "forbidden",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

type defaultACPDriver struct {
	Log       *slog.Logger
	Sandboxes sandbox.Manager
}

func (d *defaultACPDriver) RunOnce(ctx context.Context, sandboxID, cwd string, cmd []string, prompt string, actor authz.Actor) (string, string, error) {
	if d.Sandboxes == nil {
		return "", "", fmt.Errorf("sandboxes not configured")
	}
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return "", "", fmt.Errorf("empty command")
	}
	runCtx, cancel := context.WithCancel(authz.WithActor(ctx, actor))
	defer cancel()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := d.Sandboxes.AttachExec(runCtx, sandboxID, sandbox.AttachExecOpts{
			Cmd: cmd, WorkDir: cwd,
		}, stdinR, stdoutW, io.Discard)
		_ = stdinR.Close()
		_ = stdoutW.Close()
		errCh <- err
	}()

	bridge := acpclient.New(d.Log, d.Sandboxes, sandboxID, actor)
	var reply strings.Builder
	bridge.SetOnEvent(func(ev acpclient.Event) {
		if ev.Type == "agent_message" {
			reply.WriteString(ev.Text)
		}
	})
	bridge.SetPermissionHandler(autoAllowPermissions)

	conn := acp.NewClientSideConnection(bridge, stdinW, stdoutR)
	if d.Log != nil {
		conn.SetLogger(d.Log)
	}

	initCtx, initCancel := context.WithTimeout(runCtx, initTimeout)
	defer initCancel()
	if _, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		},
	}); err != nil {
		cancel()
		drainErr(errCh)
		return "", "", fmt.Errorf("acp initialize: %w", err)
	}
	if cwd == "" {
		cwd = "/workspace"
	}
	sess, err := conn.NewSession(initCtx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		cancel()
		drainErr(errCh)
		return "", "", fmt.Errorf("acp new session: %w", err)
	}

	// Probe: Initialize + NewSession only.
	if strings.TrimSpace(prompt) == "" {
		cancel()
		_ = stdinW.Close()
		drainErr(errCh)
		return "", "", nil
	}

	resp, err := conn.Prompt(runCtx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})
	stop := ""
	if err == nil {
		stop = string(resp.StopReason)
	}
	cancel()
	_ = stdinW.Close()
	drainErr(errCh)
	if err != nil {
		return reply.String(), stop, err
	}
	return reply.String(), stop, nil
}

func drainErr(ch <-chan error) {
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
	}
}

func autoAllowPermissions(req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	var allowID acp.PermissionOptionId
	for _, opt := range req.Options {
		if opt.Kind == acp.PermissionOptionKindAllowOnce || opt.Kind == acp.PermissionOptionKindAllowAlways {
			allowID = opt.OptionId
			if opt.Kind == acp.PermissionOptionKindAllowOnce {
				break
			}
		}
	}
	if allowID == "" {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{},
			},
		}, nil
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: allowID},
		},
	}, nil
}
