package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func (h *Handler) mountTasks(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/browser-tasks", h.listBrowserTasks)
	mux.HandleFunc("POST /v1/browser-tasks", h.createBrowserTask)
}

func (h *Handler) listBrowserTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Tasks == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"tasks": []any{}})
		return
	}
	list, err := h.Tasks.ListByUser(r.Context(), user.Username, 30)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if list == nil {
		list = []*browsetask.Task{}
	}
	// Prompt is only needed when starting a chat; keep the list compact.
	for _, t := range list {
		if t != nil {
			t.Prompt = ""
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tasks": list})
}

type createTaskReq struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	Brief string `json:"brief"`
}

func (h *Handler) createBrowserTask(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.ACP == nil || h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "agent sessions not configured")
		return
	}
	var req createTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	kind, err := browsetask.NormalizeKind(req.Kind)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	startURL, err := browsetask.MustStartURL(req.URL)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	brief := strings.TrimSpace(req.Brief)
	title := browsetask.TitleFor(kind, startURL)
	prompt := browsetask.PromptFor(kind, startURL, brief)

	sess, err := h.Store.Create(r.Context(), user.Username, title, "sysadmin", "", "")
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	actor := manager.Actor{
		Username: user.Username,
		Role:     string(user.Role),
		APIKey:   user.APIKey,
	}
	if _, err := h.ACP.Start(r.Context(), sess.ID, "", "sysadmin", manager.StartOpts{Actor: actor}); err != nil {
		_ = h.Store.Delete(r.Context(), sess.ID)
		httpx.WriteErr(w, http.StatusBadGateway, "start agent: "+err.Error())
		return
	}

	var task *browsetask.Task
	if h.Tasks != nil {
		task, err = h.Tasks.Create(r.Context(), user.Username, kind, startURL, brief, sess.ID, prompt)
		if err != nil {
			httpx.WriteErrOrInternal(w, r, err, nil)
			return
		}
	} else {
		task = &browsetask.Task{
			Kind: kind, URL: startURL, Brief: brief, SessionID: sess.ID, Status: "open", Prompt: prompt,
		}
	}

	if h.Envs != nil {
		userID := user.Username
		go func() {
			_, _ = h.Envs.EnsureBrowser(context.Background(), userID)
		}()
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"task":      task,
		"session":   sess,
		"prompt":    prompt,
		"sessionId": sess.ID,
	})
}
