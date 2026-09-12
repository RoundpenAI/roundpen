// Package sysagent implements the in-process System Management ACP agent.
package sysagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// keepLastToolResults is how many recent tool payloads stay verbatim in-context.
const keepLastToolResults = 6

type session struct {
	cancel     context.CancelFunc
	allowAll   bool
	allowTools map[string]bool
	denyTools  map[string]bool
}

// Deps wires LLM + tools for one agent instance.
type Deps struct {
	LLM       LLMConfig
	Tools     *tools.Registry
	Actor     tools.Actor
	History   MessageSource
	SessionID string // Roundpen agent session id (for history replay)
}

// Agent is an ACP agent that runs an OpenAI tool-calling loop.
type Agent struct {
	deps     Deps
	conn     *acp.AgentSideConnection
	sessions map[string]*session
	mu       sync.Mutex
}

var _ acp.Agent = (*Agent)(nil)

// New creates a System Agent.
func New(deps Deps) *Agent {
	if deps.Tools == nil {
		deps.Tools = tools.NewRegistry()
	}
	return &Agent{deps: deps, sessions: make(map[string]*session)}
}

// SetAgentConnection receives the connection after construction.
func (a *Agent) SetAgentConnection(conn *acp.AgentSideConnection) { a.conn = conn }

func (a *Agent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: false,
		},
	}, nil
}

func (a *Agent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sid := randomID()
	a.mu.Lock()
	a.sessions[sid] = &session{
		allowTools: make(map[string]bool),
		denyTools:  make(map[string]bool),
	}
	a.mu.Unlock()
	return acp.NewSessionResponse{SessionId: acp.SessionId(sid)}, nil
}

func (a *Agent) Authenticate(ctx context.Context, _ acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *Agent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	a.mu.Lock()
	s := a.sessions[string(params.SessionId)]
	a.mu.Unlock()
	if s != nil && s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (a *Agent) Prompt(_ context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := string(params.SessionId)
	a.mu.Lock()
	s, ok := a.sessions[sid]
	a.mu.Unlock()
	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", sid)
	}
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	s.cancel = cancel
	a.mu.Unlock()

	userText := ""
	for _, block := range params.Prompt {
		if block.Text != nil {
			userText += block.Text.Text
		}
	}

	if err := a.turn(ctx, sid, userText); err != nil {
		if ctx.Err() != nil {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		return acp.PromptResponse{}, err
	}
	a.mu.Lock()
	s.cancel = nil
	a.mu.Unlock()
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *Agent) turn(ctx context.Context, sid, userText string) error {
	messages := a.buildPromptMessages(ctx, userText)
	openaiTools := a.deps.Tools.OpenAITools()

	_ = a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update:    acp.UpdateAgentThoughtText("Working…"),
	})

	var watch loopWatch
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if historyChars(messages) > maxHistoryChars {
			messages = shrinkOldToolResults(messages, keepLastToolResults)
		}
		var streamed bool
		msg, finish, err := a.deps.LLM.chatStream(ctx, messages, openaiTools, func(chunk string) error {
			streamed = true
			return a.emitText(ctx, sid, chunk)
		})
		if err != nil {
			_ = a.emitText(ctx, sid, "LLM error: "+err.Error())
			return err
		}
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			if streamed {
				return nil
			}
			text := msg.Content
			if text == "" {
				text = "(no response)"
			}
			return a.emitText(ctx, sid, text)
		}

		var recs []toolCallRec
		for _, tc := range msg.ToolCalls {
			name := tc.Function.Name
			args := json.RawMessage(tc.Function.Arguments)
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			var argsObj any
			if err := json.Unmarshal(args, &argsObj); err != nil {
				argsObj = map[string]any{"raw": string(args)}
			}
			tool, ok := a.deps.Tools.Get(name)
			title := name
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(sid),
				Update: acp.StartToolCall(
					acp.ToolCallId(tc.ID),
					title,
					acp.WithStartKind(acp.ToolKindOther),
					acp.WithStartStatus(acp.ToolCallStatusPending),
					acp.WithStartRawInput(argsObj),
				),
			}); err != nil {
				return err
			}

			allowed := true
			if ok && tool.Mutating {
				if decided, ok := a.toolPermCached(sid, name); ok {
					allowed = decided
				} else {
					opts := []acp.PermissionOption{
						{OptionId: acp.PermissionOptionId("allow"), Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
						{OptionId: acp.PermissionOptionId("allow_tool"), Name: "Allow this tool (session)", Kind: acp.PermissionOptionKindAllowAlways},
						{OptionId: acp.PermissionOptionId("allow_all"), Name: "Allow all tools (session)", Kind: acp.PermissionOptionKindAllowAlways},
						{OptionId: acp.PermissionOptionId("reject"), Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
						{OptionId: acp.PermissionOptionId("reject_tool"), Name: "Reject this tool (session)", Kind: acp.PermissionOptionKindRejectAlways},
					}
					resp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
						SessionId: acp.SessionId(sid),
						ToolCall: acp.ToolCallUpdate{
							ToolCallId: acp.ToolCallId(tc.ID),
							Title:      &title,
						},
						Options: opts,
					})
					if err != nil {
						return err
					}
					opt := ""
					if resp.Outcome.Selected != nil {
						opt = string(resp.Outcome.Selected.OptionId)
					}
					allowed = a.applyToolPerm(sid, name, opt)
				}
			}

			var result string
			var callErr error
			status := acp.ToolCallStatusCompleted
			if !allowed {
				result = "Permission rejected by user."
				status = acp.ToolCallStatusFailed
			} else {
				result, callErr = a.deps.Tools.Call(ctx, a.deps.Actor, name, args)
				if callErr != nil {
					status = acp.ToolCallStatusFailed
					if strings.TrimSpace(result) == "" {
						result = tools.FormatToolError(callErr)
					}
				}
			}
			var outObj any = result
			var parsed any
			if err := json.Unmarshal([]byte(result), &parsed); err == nil {
				outObj = parsed
			}
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(sid),
				Update: acp.UpdateToolCall(
					acp.ToolCallId(tc.ID),
					acp.WithUpdateStatus(status),
					acp.WithUpdateRawOutput(outObj),
					acp.WithUpdateContent([]acp.ToolCallContent{
						acp.ToolContent(acp.TextBlock(result)),
					}),
				),
			}); err != nil {
				return err
			}
			messages = append(messages, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       name,
				Content:    result,
			})
			recs = append(recs, toolCallRec{name: name, args: string(args), result: result})
		}
		_ = finish
		nudge, stop := watch.observe(recs)
		if stop {
			return a.emitText(ctx, sid, watch.stopText())
		}
		if nudge {
			thought := "Same actions repeating — changing approach."
			if watch.lastKind == "agent" {
				thought = "Agent workspace is absent — use Bash or a file tool; do not stop after listing."
			} else if watch.infra > 0 {
				thought = "Browser Chrome is not ready — wait and retry browser_* once, do not keep clicking."
			}
			_ = a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(sid),
				Update:    acp.UpdateAgentThoughtText(thought),
			})
			messages = append(messages, chatMessage{Role: "user", Content: watch.nudgeText()})
		}
	}
}

func (a *Agent) emitText(ctx context.Context, sid, text string) error {
	return a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update:    acp.UpdateAgentMessageText(text),
	})
}

// toolPermCached returns a prior session decision for a mutating tool, if any.
func (a *Agent) toolPermCached(sid, toolName string) (allowed bool, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	if s == nil {
		return false, false
	}
	if s.denyTools[toolName] {
		return false, true
	}
	if s.allowAll || s.allowTools[toolName] {
		return true, true
	}
	return false, false
}

// applyToolPerm records the user's choice and returns whether the call may proceed.
func (a *Agent) applyToolPerm(sid, toolName, optionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	if s == nil {
		return optionID == "allow" || optionID == "allow_tool" || optionID == "allow_all"
	}
	switch optionID {
	case "allow":
		return true
	case "allow_tool":
		s.allowTools[toolName] = true
		delete(s.denyTools, toolName)
		return true
	case "allow_all":
		s.allowAll = true
		return true
	case "reject_tool":
		s.denyTools[toolName] = true
		delete(s.allowTools, toolName)
		return false
	default: // reject / cancelled / unknown
		return false
	}
}

func (a *Agent) Logout(ctx context.Context, params acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}
func (a *Agent) CloseSession(ctx context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionClose)
}
func (a *Agent) ListSessions(ctx context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}
func (a *Agent) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetConfigOption)
}
func (a *Agent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// LoopbackBase turns an HTTP listen addr into a loopback URL.
func LoopbackBase(httpAddr string) string {
	host := strings.TrimSpace(httpAddr)
	if host == "" {
		return "http://127.0.0.1:9527"
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	if strings.HasPrefix(host, ":") {
		return "http://127.0.0.1" + host
	}
	if strings.HasPrefix(host, "0.0.0.0:") {
		return "http://127.0.0.1" + host[len("0.0.0.0"):]
	}
	if strings.HasPrefix(host, "[::]:") {
		return "http://127.0.0.1" + host[len("[::]"):]
	}
	return "http://" + host
}
