package sysagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/toolgw"
)

// maxParallelTools caps concurrent safe tool invokes in one wave.
const maxParallelTools = 8

// preparedCall is one tool_call after UI start + permission gates, before invoke.
type preparedCall struct {
	tc              toolCall
	name            string
	args            json.RawMessage
	title           string
	mutating        bool
	autoVerdict     automode.Verdict
	result          string
	status          acp.ToolCallStatus
	ready           bool // true → invoke; false → result/status already set
	parallelSafe    bool
	skillAfterAllow bool // count toward skillInvocations when invoke succeeds
}

// runToolBatch executes a model tool_calls list. Consecutive read-only, already-
// allowed tools run concurrently (finish-as-complete); mutating / browser /
// Skill / permissioned calls stay serial. Results append to messages in the
// original tool_call order.
func (a *Agent) runToolBatch(
	ctx context.Context,
	sid string,
	calls []toolCall,
	messages []chatMessage,
	skillInvocations *int,
) (recs []toolCallRec, outMessages []chatMessage, err error) {
	outMessages = messages
	if len(calls) == 0 {
		return nil, outMessages, nil
	}

	callIdx := 0
	var hold []preparedCall
	take := func() (preparedCall, bool, error) {
		if len(hold) > 0 {
			p := hold[0]
			hold = hold[1:]
			return p, true, nil
		}
		if callIdx >= len(calls) {
			return preparedCall{}, false, nil
		}
		p, err := a.prepareToolCall(ctx, sid, calls[callIdx], outMessages, *skillInvocations)
		callIdx++
		if err != nil {
			return preparedCall{}, false, err
		}
		return p, true, nil
	}

	for {
		prep, ok, err := take()
		if err != nil {
			return recs, outMessages, err
		}
		if !ok {
			return recs, outMessages, nil
		}

		if prep.ready && prep.parallelSafe {
			wave := []preparedCall{prep}
			for len(wave) < maxParallelTools {
				next, ok, err := take()
				if err != nil {
					return recs, outMessages, err
				}
				if !ok {
					break
				}
				if next.ready && next.parallelSafe {
					wave = append(wave, next)
					continue
				}
				hold = append([]preparedCall{next}, hold...)
				break
			}
			recs, outMessages, err = a.finishWave(ctx, sid, wave, outMessages, skillInvocations, recs)
			if err != nil {
				return recs, outMessages, err
			}
			continue
		}

		recs, outMessages, err = a.finishOne(ctx, sid, prep, outMessages, skillInvocations, recs)
		if err != nil {
			return recs, outMessages, err
		}
	}
}

func (a *Agent) prepareToolCall(
	ctx context.Context,
	sid string,
	tc toolCall,
	messages []chatMessage,
	skillInvocations int,
) (preparedCall, error) {
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
		return preparedCall{}, err
	}

	mutating := ok && tool.Mutating
	if name == "Skill" {
		mutating = skillMutates(args)
	}
	blockedByPlanMode := mutating && a.inPlanMode(sid)
	skillBlocked := name == "Skill" && skillInvocations >= maxSkillInvocationsPerTurn

	out := preparedCall{
		tc:              tc,
		name:            name,
		args:            args,
		title:           title,
		mutating:        mutating,
		status:          acp.ToolCallStatusCompleted,
		autoVerdict:     automode.Verdict{Decision: automode.Allow},
		skillAfterAllow: name == "Skill",
	}

	allowed := true
	autoDenied := false
	autoDenyReason := ""
	if !blockedByPlanMode && mutating {
		if decided, ok := a.toolPermCached(sid, name); ok {
			allowed = decided
		} else {
			mode := a.permMode(sid)
			req := automode.Request{
				Name:       name,
				Title:      title,
				Args:       string(args),
				UserDigest: turnDigest(messages),
			}
			switch {
			case mode == automode.ModeAll ||
				(mode == automode.ModeEdit && automode.IsWorkspaceEdit(name, "")):
				if v, ok := automode.PreCheck(req); ok {
					allowed = false
					autoDenied = true
					autoDenyReason = formatAutoReason(v)
					out.autoVerdict = v
				} else {
					allowed = true
				}
			case mode == automode.ModeDecide:
				res := automode.Resolve(ctx, a.deps.Evaluator, req)
				out.autoVerdict = res.Verdict
				if res.EvalErr != nil {
					slog.Default().Warn("auto mode classifier failed",
						slog.String("session", sid), slog.String("tool", name), slog.Any("err", res.EvalErr))
				}
				switch {
				case res.Verdict.Decision == automode.HardDeny:
					allowed = false
					autoDenied = true
					autoDenyReason = formatAutoReason(res.Verdict)
				case res.AskHuman:
					askTitle := annotatePermissionTitle(title, res.Verdict)
					var err error
					allowed, err = a.askToolPermission(ctx, sid, tc.ID, name, askTitle, res.Verdict)
					if err != nil {
						_ = a.finishToolCall(ctx, sid, tc.ID, acp.ToolCallStatusFailed, "Tool call cancelled.")
						return preparedCall{}, err
					}
				case res.Allowed():
					allowed = true
					_ = a.annotateAutoApproved(ctx, sid, tc.ID, title, res.Verdict)
				default:
					allowed = false
					autoDenied = true
					autoDenyReason = formatAutoReason(res.Verdict)
				}
			default:
				var err error
				allowed, err = a.askToolPermission(ctx, sid, tc.ID, name, title, automode.Verdict{})
				if err != nil {
					_ = a.finishToolCall(ctx, sid, tc.ID, acp.ToolCallStatusFailed, "Tool call cancelled.")
					return preparedCall{}, err
				}
			}
		}
	}

	switch {
	case blockedByPlanMode:
		out.result = fmt.Sprintf("Blocked: %s is unavailable while in plan mode. "+
			"You must only explore and design; call ExitPlanMode to present the plan for approval and start implementing.", name)
		out.status = acp.ToolCallStatusFailed
	case skillBlocked:
		out.result = fmt.Sprintf("Blocked: Skill was invoked %d times in this reply (nesting limit). "+
			"Finish the current skill's steps with the tools already available; "+
			"if more skills are genuinely needed, end this reply so the user can send a follow-up message.", maxSkillInvocationsPerTurn)
		out.status = acp.ToolCallStatusFailed
	case autoDenied:
		out.result = fmt.Sprintf("Blocked by auto mode: %s. Do not retry this action or route around it "+
			"with another tool; choose a safer approach that stays inside the workspace.", autoDenyReason)
		out.status = acp.ToolCallStatusFailed
	case !allowed:
		out.result = "Permission rejected by user."
		out.status = acp.ToolCallStatusFailed
	default:
		out.ready = true
		out.parallelSafe = toolParallelSafe(name, mutating)
	}
	return out, nil
}

// toolParallelSafe reports whether a ready call may share a concurrent wave.
// Browser tools share one CDP session; Skill has a per-turn cap — keep both serial.
func toolParallelSafe(name string, mutating bool) bool {
	if mutating {
		return false
	}
	if name == "Skill" || strings.HasPrefix(name, "browser_") {
		return false
	}
	return true
}

func (a *Agent) finishOne(
	ctx context.Context,
	sid string,
	prep preparedCall,
	messages []chatMessage,
	skillInvocations *int,
	recs []toolCallRec,
) ([]toolCallRec, []chatMessage, error) {
	if prep.ready {
		if err := a.setToolStatus(ctx, sid, prep.tc.ID, acp.ToolCallStatusInProgress); err != nil {
			return recs, messages, err
		}
		result, status := a.invokePrepared(ctx, sid, prep)
		prep.result = result
		prep.status = status
		if prep.skillAfterAllow && status == acp.ToolCallStatusCompleted {
			*skillInvocations++
		}
	}
	if err := a.finishToolCall(ctx, sid, prep.tc.ID, prep.status, prep.result); err != nil {
		return recs, messages, err
	}
	messages = append(messages, chatMessage{
		Role:       "tool",
		ToolCallID: prep.tc.ID,
		Name:       prep.name,
		Content:    prep.result,
	})
	recs = append(recs, toolCallRec{name: prep.name, args: string(prep.args), result: prep.result})
	return recs, messages, nil
}

func (a *Agent) finishWave(
	ctx context.Context,
	sid string,
	wave []preparedCall,
	messages []chatMessage,
	skillInvocations *int,
	recs []toolCallRec,
) ([]toolCallRec, []chatMessage, error) {
	if len(wave) == 0 {
		return recs, messages, nil
	}
	if len(wave) == 1 {
		return a.finishOne(ctx, sid, wave[0], messages, skillInvocations, recs)
	}

	names := make([]string, 0, len(wave))
	for _, p := range wave {
		names = append(names, p.name)
	}
	_ = a.emitThought(ctx, sid, fmt.Sprintf("Running %d tools in parallel: %s", len(wave), strings.Join(names, ", ")))

	for _, p := range wave {
		if err := a.setToolStatus(ctx, sid, p.tc.ID, acp.ToolCallStatusInProgress); err != nil {
			return recs, messages, err
		}
	}

	type outcome struct {
		result string
		status acp.ToolCallStatus
	}
	outcomes := make([]outcome, len(wave))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallelTools)
	errCh := make(chan error, 1)

	for idx := range wave {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				outcomes[i] = outcome{result: "Tool call cancelled.", status: acp.ToolCallStatusFailed}
				return
			}
			res, st := a.invokePrepared(ctx, sid, wave[i])
			outcomes[i] = outcome{result: res, status: st}
			// Finish as soon as this call completes so the UI lights up live.
			if err := a.finishToolCall(ctx, sid, wave[i].tc.ID, st, res); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}(idx)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return recs, messages, err
	default:
	}

	for i, p := range wave {
		out := outcomes[i]
		if p.skillAfterAllow && out.status == acp.ToolCallStatusCompleted {
			*skillInvocations++
		}
		messages = append(messages, chatMessage{
			Role:       "tool",
			ToolCallID: p.tc.ID,
			Name:       p.name,
			Content:    out.result,
		})
		recs = append(recs, toolCallRec{name: p.name, args: string(p.args), result: out.result})
	}
	return recs, messages, nil
}

func (a *Agent) invokePrepared(ctx context.Context, sid string, prep preparedCall) (string, acp.ToolCallStatus) {
	toolCtx := tools.WithInteractor(ctx, a.interactor(sid))
	inv := a.deps.Gateway.Invoke(toolCtx, toolgw.Call{
		Actor:       a.deps.Actor,
		SessionID:   a.deps.SessionID,
		AssistantID: a.deps.AssistantID,
		Name:        prep.name,
		Args:        prep.args,
		AutoVerdict: prep.autoVerdict,
		Policy:      a.effectivePolicy(ctx),
	})
	result := inv.Content
	status := acp.ToolCallStatusCompleted
	if inv.Decision == toolgw.DecisionDeny {
		status = acp.ToolCallStatusFailed
	} else if inv.Err != nil {
		status = acp.ToolCallStatusFailed
		if strings.TrimSpace(result) == "" {
			result = tools.FormatToolError(inv.Err)
		}
	}
	return result, status
}

func (a *Agent) setToolStatus(ctx context.Context, sid, callID string, status acp.ToolCallStatus) error {
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.conn.SessionUpdate(updateCtx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.UpdateToolCall(
			acp.ToolCallId(callID),
			acp.WithUpdateStatus(status),
		),
	})
}
