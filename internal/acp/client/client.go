// Package client implements an ACP Client that forwards updates and permissions to the Web UI.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// Event is a simplified session update for the WebSocket UI.
type Event struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Title     string `json:"title,omitempty"`
	Status    string `json:"status,omitempty"`
	Kind      string `json:"kind,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	Input     any    `json:"input,omitempty"`
	Output    any    `json:"output,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// Bridge is an ACP Client backed by Roundpen workspace files.
type Bridge struct {
	log       *slog.Logger
	sandboxes sandbox.Manager
	sandboxID string

	mu                sync.Mutex
	onEvent           func(Event)
	permissionHandler func(acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)
	autoApprove       bool
}

var _ acp.Client = (*Bridge)(nil)

// New creates a Bridge.
func New(log *slog.Logger, sandboxes sandbox.Manager, sandboxID string, autoApprove bool) *Bridge {
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{log: log, sandboxes: sandboxes, sandboxID: sandboxID, autoApprove: autoApprove}
}

// SetOnEvent sets the live event sink.
func (b *Bridge) SetOnEvent(fn func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onEvent = fn
}

// SetPermissionHandler sets interactive permission handling.
func (b *Bridge) SetPermissionHandler(fn func(acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.permissionHandler = fn
}

// SetAutoApprove toggles automatic approval of ordinary permission options.
func (b *Bridge) SetAutoApprove(v bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.autoApprove = v
}

func (b *Bridge) emit(ev Event) {
	b.mu.Lock()
	fn := b.onEvent
	b.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

func (b *Bridge) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	u := params.Update
	sid := string(params.SessionId)
	switch {
	case u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil:
		b.emit(Event{Type: "agent_message", Text: u.AgentMessageChunk.Content.Text.Text, SessionID: sid})
	case u.AgentThoughtChunk != nil && u.AgentThoughtChunk.Content.Text != nil:
		b.emit(Event{Type: "agent_thought", Text: u.AgentThoughtChunk.Content.Text.Text, SessionID: sid})
	case u.ToolCall != nil:
		b.emit(Event{
			Type: "tool_call", Title: u.ToolCall.Title, Status: string(u.ToolCall.Status),
			Kind: string(u.ToolCall.Kind), ToolID: string(u.ToolCall.ToolCallId),
			Input: u.ToolCall.RawInput, Output: u.ToolCall.RawOutput, SessionID: sid,
		})
	case u.ToolCallUpdate != nil:
		status := ""
		if u.ToolCallUpdate.Status != nil {
			status = string(*u.ToolCallUpdate.Status)
		}
		title := ""
		if u.ToolCallUpdate.Title != nil {
			title = *u.ToolCallUpdate.Title
		}
		kind := ""
		if u.ToolCallUpdate.Kind != nil {
			kind = string(*u.ToolCallUpdate.Kind)
		}
		b.emit(Event{
			Type: "tool_call_update", Title: title, Status: status, Kind: kind,
			ToolID: string(u.ToolCallUpdate.ToolCallId),
			Input: u.ToolCallUpdate.RawInput, Output: u.ToolCallUpdate.RawOutput, SessionID: sid,
		})
	case u.Plan != nil:
		b.emit(Event{Type: "plan", Text: fmt.Sprintf("%d steps", len(u.Plan.Entries)), SessionID: sid})
	}
	return nil
}

func (b *Bridge) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	b.mu.Lock()
	auto := b.autoApprove
	fn := b.permissionHandler
	b.mu.Unlock()
	if auto {
		if opt := PickOrdinaryAllow(params.Options); opt != "" {
			title := ""
			if params.ToolCall.Title != nil {
				title = *params.ToolCall.Title
			}
			b.emit(Event{
				Type:   "permission",
				Title:  title,
				Text:   opt,
				Status: "auto",
				ToolID: string(params.ToolCall.ToolCallId),
			})
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(opt)},
				},
			}, nil
		}
	}
	if fn == nil {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
		}, nil
	}
	return fn(params)
}

func (b *Bridge) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	rel, err := guestToRel(params.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	rc, err := b.sandboxes.ReadFile(ctx, b.sandboxID, rel)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	defer rc.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, rerr := rc.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	return acp.ReadTextFileResponse{Content: string(buf)}, nil
}

func (b *Bridge) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	rel, err := guestToRel(params.Path)
	if err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := b.sandboxes.WriteFile(ctx, b.sandboxID, rel, strings.NewReader(params.Content)); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (b *Bridge) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound("terminal/create")
}
func (b *Bridge) KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound("terminal/kill")
}
func (b *Bridge) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound("terminal/output")
}
func (b *Bridge) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound("terminal/release")
}
func (b *Bridge) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound("terminal/wait_for_exit")
}

func guestToRel(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(path, "/workspace/") {
		return filepath.Clean(strings.TrimPrefix(path, "/workspace/")), nil
	}
	if path == "/workspace" {
		return ".", nil
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be under /workspace: %s", path)
	}
	return filepath.Clean(path), nil
}
