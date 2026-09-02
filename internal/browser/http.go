package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const defaultCategory = "Browser"

// SandboxView is the sandbox lookup surface the browser handler needs.
type SandboxView interface {
	Get(ctx context.Context, id string) (*sandbox.Sandbox, error)
	Resolve(ctx context.Context, req sandbox.ResolveRequest) (*sandbox.Sandbox, error)
	Touch(ctx context.Context, id string) error
}

// Handler serves REST browser tools and a Streamable HTTP MCP endpoint.
type Handler struct {
	Sandboxes SandboxView
	Hub       *Hub
}

// Mount registers browser routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sandboxes/{id}/browser", h.withSandbox(h.status))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/navigate", h.withSandbox(h.navigate))
	mux.HandleFunc("GET /v1/sandboxes/{id}/browser/snapshot", h.withSandbox(h.snapshot))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/snapshot", h.withSandbox(h.snapshot))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/click", h.withSandbox(h.click))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/type", h.withSandbox(h.typ))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/press", h.withSandbox(h.press))
	mux.HandleFunc("GET /v1/sandboxes/{id}/browser/screenshot", h.withSandbox(h.screenshot))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/viewport", h.withSandbox(h.viewport))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/evaluate", h.withSandbox(h.evaluate))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/close", h.withSandbox(h.closeSess))
	mux.HandleFunc("POST /v1/sandboxes/{id}/browser/mcp", h.withSandbox(h.mcp))
	mux.HandleFunc("GET /v1/sandboxes/{id}/browser/mcp", h.withSandbox(h.mcp))

	mux.HandleFunc("GET /v1/browser", h.withDefault(h.status))
	mux.HandleFunc("POST /v1/browser/navigate", h.withDefault(h.navigate))
	mux.HandleFunc("GET /v1/browser/snapshot", h.withDefault(h.snapshot))
	mux.HandleFunc("POST /v1/browser/snapshot", h.withDefault(h.snapshot))
	mux.HandleFunc("POST /v1/browser/click", h.withDefault(h.click))
	mux.HandleFunc("POST /v1/browser/type", h.withDefault(h.typ))
	mux.HandleFunc("POST /v1/browser/press", h.withDefault(h.press))
	mux.HandleFunc("GET /v1/browser/screenshot", h.withDefault(h.screenshot))
	mux.HandleFunc("POST /v1/browser/viewport", h.withDefault(h.viewport))
	mux.HandleFunc("POST /v1/browser/evaluate", h.withDefault(h.evaluate))
	mux.HandleFunc("POST /v1/browser/close", h.withDefault(h.closeSess))
	mux.HandleFunc("POST /v1/browser/mcp", h.withDefault(h.mcp))
	mux.HandleFunc("GET /v1/browser/mcp", h.withDefault(h.mcp))
}

type browserCtx struct {
	id string
	sb *sandbox.Sandbox
}

type handlerFunc func(http.ResponseWriter, *http.Request, browserCtx)

func (h *Handler) withSandbox(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sb, err := h.Sandboxes.Get(r.Context(), id)
		if errors.Is(err, sandbox.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "sandbox not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sb.Status != sandbox.StatusRunning {
			writeErr(w, http.StatusConflict, "sandbox is "+string(sb.Status))
			return
		}
		next(w, r, browserCtx{id: id, sb: sb})
	}
}

func (h *Handler) withDefault(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat := strings.TrimSpace(r.URL.Query().Get("category"))
		if cat == "" {
			cat = defaultCategory
		}
		sb, err := h.Sandboxes.Resolve(r.Context(), sandbox.ResolveRequest{
			Name:     strings.TrimSpace(r.URL.Query().Get("name")),
			Category: cat,
		})
		if errors.Is(err, sandbox.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "no "+cat+" sandbox")
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if sb.Status != sandbox.StatusRunning {
			writeErr(w, http.StatusConflict, "sandbox is "+string(sb.Status))
			return
		}
		next(w, r, browserCtx{id: sb.ID, sb: sb})
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	attached, url, width, height := h.Hub.Status(bc.id)
	writeJSON(w, http.StatusOK, map[string]any{
		"sandboxID": bc.id,
		"name":      bc.sb.Name,
		"category":  bc.sb.Category,
		"profile":   metadataProfile(bc.sb),
		"attached":  attached,
		"url":       url,
		"width":     width,
		"height":    height,
		"mcp":       "/v1/sandboxes/" + bc.id + "/browser/mcp",
		"tools":     []string{"navigate", "snapshot", "click", "type", "press", "screenshot", "viewport", "evaluate"},
	})
}

type navigateReq struct {
	URL string `json:"url"`
}

func (h *Handler) navigate(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	var req navigateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sess.Engine.Navigate(r.Context(), req.URL); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = h.Sandboxes.Touch(r.Context(), bc.id)
	snap, err := sess.Engine.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": sess.Engine.URL()})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	snap, err := sess.Engine.Snapshot(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

type clickReq struct {
	Ref string `json:"ref"`
}

func (h *Handler) click(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	var req clickReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ref) == "" {
		writeErr(w, http.StatusBadRequest, "ref is required")
		return
	}
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sess.Engine.Click(r.Context(), req.Ref); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = h.Sandboxes.Touch(r.Context(), bc.id)
	snap, err := sess.Engine.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": sess.Engine.URL()})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

type typeReq struct {
	Ref    string `json:"ref"`
	Text   string `json:"text"`
	Submit bool   `json:"submit"`
}

func (h *Handler) typ(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	var req typeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ref) == "" {
		writeErr(w, http.StatusBadRequest, "ref is required")
		return
	}
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sess.Engine.Type(r.Context(), req.Ref, req.Text, req.Submit); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = h.Sandboxes.Touch(r.Context(), bc.id)
	snap, err := sess.Engine.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

type pressReq struct {
	Key string `json:"key"`
}

func (h *Handler) press(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	var req pressReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Key) == "" {
		writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sess.Engine.Press(r.Context(), req.Key); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": sess.Engine.URL()})
}

func (h *Handler) screenshot(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	png, err := sess.Engine.Screenshot(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"mimeType": "image/png",
			"data":     base64.StdEncoding.EncodeToString(png),
			"url":      sess.Engine.URL(),
		})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

type viewportReq struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (h *Handler) viewport(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	var req viewportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sess.Engine.SetViewport(r.Context(), req.Width, req.Height); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sess.Width, sess.Height = req.Width, req.Height
	writeJSON(w, http.StatusOK, map[string]any{"width": req.Width, "height": req.Height})
}

type evalReq struct {
	Expression string `json:"expression"`
}

func (h *Handler) evaluate(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	var req evalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Expression) == "" {
		writeErr(w, http.StatusBadRequest, "expression is required")
		return
	}
	sess, err := h.Hub.Ensure(r.Context(), bc.id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	raw, err := sess.Engine.Evaluate(r.Context(), req.Expression)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": json.RawMessage(raw)})
}

func (h *Handler) closeSess(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	h.Hub.CloseSandbox(bc.id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func metadataProfile(sb *sandbox.Sandbox) string {
	if sb == nil || sb.Metadata == nil {
		return ""
	}
	return sb.Metadata["profile"]
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
