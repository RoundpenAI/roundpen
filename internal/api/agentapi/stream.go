package agentapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// streamClient is a fan-out sink for one browser/mobile attachment. Frames are
// queued and drained by the SSE writer (or tests).
type streamClient struct {
	mu     sync.Mutex
	ch     chan any
	closed bool
}

func newStreamClient(buffer int) *streamClient {
	if buffer < 1 {
		buffer = 64
	}
	return &streamClient{ch: make(chan any, buffer)}
}

func (c *streamClient) write(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.ch <- v:
	default:
		// Slow consumer: drop the attachment rather than block the runner.
		c.closed = true
		close(c.ch)
	}
}

func (c *streamClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.ch)
}

type streamIn struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Name      string `json:"name,omitempty"`
	Args      string `json:"args,omitempty"`
	OptionID  string `json:"optionId,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	Enabled   bool   `json:"enabled"`
	// ID is a queue item reference (unqueue); ClientMsgID echoes the sender's
	// optimistic row so the queue snapshot can be matched per client.
	ID          string `json:"id,omitempty"`
	ClientMsgID string `json:"clientMsgId,omitempty"`
}

type streamOut struct {
	Type       string `json:"type"`
	Event      any    `json:"event,omitempty"`
	Message    string `json:"message,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Title      string `json:"title,omitempty"`
	Options    any    `json:"options,omitempty"`
	TicketID   string `json:"ticketId,omitempty"`
}

func (h *Handler) loadSessionForUser(w http.ResponseWriter, r *http.Request) (*agentsession.Session, *storage.User, bool) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, nil, false
	}
	id := r.PathValue("id")
	sess, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, agentsession.ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return nil, nil, false
	}
	if err != nil || (sess.UserID != user.Username && user.Role != "admin") {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return nil, nil, false
	}
	return sess, user, true
}

func (h *Handler) ensureRunnerReady(w http.ResponseWriter, sess *agentsession.Session) bool {
	if _, ok := h.ACP.Get(sess.ID); ok {
		return true
	}
	provMeta, found := providers.ByID(h.ACP.Providers(), sess.ProviderID)
	if !found {
		httpx.WriteErr(w, http.StatusConflict, "unknown provider")
		return false
	}
	if providers.NeedsSandbox(provMeta) && sess.SandboxID == "" {
		httpx.WriteErr(w, http.StatusConflict, "agent runtime not running")
		return false
	}
	return true
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// sessionEvents streams session frames as text/event-stream (SSE).
// Client→server actions use POST /v1/agent-sessions/{id}/actions.
func (h *Handler) sessionEvents(w http.ResponseWriter, r *http.Request) {
	sess, user, ok := h.loadSessionForUser(w, r)
	if !ok {
		return
	}
	if !h.ensureRunnerReady(w, sess) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	client := newStreamClient(128)
	_ = writeSSE(w, flusher, streamOut{Type: "hello", Message: "ok"})

	run, runErr := h.runnerFor(sess, user)
	if runErr != nil {
		_ = writeSSE(w, flusher, streamOut{Type: "error", Message: "restart agent: " + runErr.Error()})
		client.close()
		return
	}

	_ = writeSSE(w, flusher, run.snapshot())
	run.subscribe(client)
	defer func() {
		run.unsubscribe(client)
		client.close()
	}()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case v, ok := <-client.ch:
			if !ok {
				return
			}
			if err := writeSSE(w, flusher, v); err != nil {
				return
			}
		}
	}
}

// sessionAction applies one client→server chat control frame over HTTP.
func (h *Handler) sessionAction(w http.ResponseWriter, r *http.Request) {
	sess, user, ok := h.loadSessionForUser(w, r)
	if !ok {
		return
	}
	if !h.ensureRunnerReady(w, sess) {
		return
	}

	var in streamIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	run, runErr := h.runnerFor(sess, user)
	if runErr != nil {
		httpx.WriteErr(w, http.StatusConflict, "restart agent: "+runErr.Error())
		return
	}

	// Ephemeral sink for unqueue errors directed at this requester only.
	reply := newStreamClient(4)
	defer reply.close()

	switch strings.ToLower(strings.TrimSpace(in.Type)) {
	case "prompt":
		run.prompt(in.Text, in.ClientMsgID)
	case "steer":
		run.steer(in.Text, in.ClientMsgID)
	case "unqueue":
		run.unqueue(in.ID, reply)
	case "command":
		run.command(in.Name, in.Args)
	case "cancel":
		run.cancelTurn()
	case "auto":
		run.setAuto(in.Enabled)
	case "permission":
		run.answerPermission(in.RequestID, in.OptionID)
	default:
		httpx.WriteErr(w, http.StatusBadRequest, "unknown action type")
		return
	}

	// Drain targeted errors (unqueue) into the HTTP response when present.
	select {
	case v := <-reply.ch:
		if out, ok := v.(streamOut); ok && out.Type == "error" {
			httpx.WriteErr(w, http.StatusConflict, out.Message)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, v)
		return
	default:
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
