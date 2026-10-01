package webpush

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// Handler serves /v1/me/push/* for subscription management.
type Handler struct {
	Svc *Service
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/push/vapid-key", h.vapidKey)
	mux.HandleFunc("GET /v1/me/push/status", h.status)
	mux.HandleFunc("POST /v1/me/push/subscriptions", h.subscribe)
	mux.HandleFunc("DELETE /v1/me/push/subscriptions", h.unsubscribe)
}

func (h *Handler) vapidKey(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r.Context()) == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil || !h.Svc.Configured() {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "web push not configured")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"publicKey": h.Svc.VAPID.PublicKey,
	})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	configured := h.Svc != nil && h.Svc.Configured()
	subscribed := false
	if configured && h.Svc.Store != nil {
		ok, err := h.Svc.Store.HasAny(r.Context(), user.Username)
		if err != nil {
			httpx.WriteErrOrInternal(w, r, err, nil)
			return
		}
		subscribed = ok
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"configured": configured,
		"subscribed": subscribed,
	})
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil || h.Svc.Store == nil || !h.Svc.Configured() {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "web push not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sub, err := h.Svc.Store.Upsert(r.Context(), user.Username, UpsertInput{
		Endpoint:  body.Endpoint,
		P256dh:    body.Keys.P256dh,
		Auth:      body.Keys.Auth,
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		if err == errInvalidSubscription {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"subscription": sub})
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil || h.Svc.Store == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Endpoint) == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	if err := h.Svc.Store.Delete(r.Context(), user.Username, body.Endpoint); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
