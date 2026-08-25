package memory

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Handler exposes memory REST endpoints.
//
// Agent-facing long-term API (mem0-inspired):
//
//	POST   /v1/memories/add
//	POST   /v1/memories/search
//	GET    /v1/memories
//	GET    /v1/memories/{id}
//	PUT    /v1/memories/{id}
//	DELETE /v1/memories/{id}
//
// Session short-term (working memory):
//
//	GET/POST /v1/sessions/{sid}/memory
//	DELETE   /v1/sessions/{sid}/memory/{id}
type Handler struct {
	Store   Store
	Service *Service // required for add/search auto-embed; falls back to Store-only
}

// Mount registers memory routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sessions/{sid}/memory", h.listShort)
	mux.HandleFunc("POST /v1/sessions/{sid}/memory", h.putShort)
	mux.HandleFunc("DELETE /v1/sessions/{sid}/memory/{id}", h.deleteShort)

	mux.HandleFunc("POST /v1/memories/add", h.addMemory)
	mux.HandleFunc("POST /v1/memories/search", h.searchMemory)
	mux.HandleFunc("GET /v1/memories", h.listMemories)
	mux.HandleFunc("GET /v1/memories/{id}", h.getMemory)
	mux.HandleFunc("PUT /v1/memories/{id}", h.updateMemory)
	mux.HandleFunc("DELETE /v1/memories/{id}", h.deleteMemory)
}

func (h *Handler) svc() *Service {
	if h.Service != nil {
		return h.Service
	}
	return &Service{Store: h.Store}
}

// --- short-term ---

type putShortReq struct {
	ID        string          `json:"id"`
	Payload   json.RawMessage `json:"payload"`
	ExpiresAt *time.Time      `json:"expires_at"`
}

func (h *Handler) listShort(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Store.ListShort(r.Context(), r.PathValue("sid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if entries == nil {
		entries = []ShortEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handler) putShort(w http.ResponseWriter, r *http.Request) {
	var req putShortReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	id := req.ID
	if id == "" {
		id = uuid.NewString()
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	e := ShortEntry{
		ID: id, SessionID: r.PathValue("sid"), Payload: payload,
		ExpiresAt: req.ExpiresAt, CreatedAt: time.Now().UTC(),
	}
	if err := h.Store.PutShort(r.Context(), e); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (h *Handler) deleteShort(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteShort(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- mem0-style long-term ---

type addMemoryReq struct {
	Messages   []Message         `json:"messages"`
	Text       string            `json:"text"`
	Memory     string            `json:"memory"` // alias of text (mem0 get response field)
	AgentID    string            `json:"agent_id"`
	UserID     string            `json:"user_id"`
	RunID      string            `json:"run_id"`
	Kind       LongKind          `json:"kind"`
	Categories []string          `json:"categories"` // first maps to kind when kind empty
	Metadata   map[string]string `json:"metadata"`
	Importance int               `json:"importance"`
	ExpiresAt  *time.Time        `json:"expiration_date"`
	Embedding  []float32         `json:"embedding"`
	Infer      bool              `json:"infer"`
}

func (h *Handler) addMemory(w http.ResponseWriter, r *http.Request) {
	var req addMemoryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	text := req.Text
	if text == "" {
		text = req.Memory
	}
	kind := req.Kind
	if kind == "" && len(req.Categories) > 0 {
		kind = LongKind(req.Categories[0])
	}
	e, err := h.svc().Add(r.Context(), AddInput{
		Text: text, Messages: req.Messages,
		AgentID: req.AgentID, UserID: req.UserID, RunID: req.RunID,
		Kind: kind, Metadata: req.Metadata, Importance: req.Importance,
		ExpiresAt: req.ExpiresAt, Embedding: req.Embedding, Infer: req.Infer,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toAgentMemory(e, 0))
}

type searchMemoryReq struct {
	Query     string         `json:"query"`
	Filters   map[string]any `json:"filters"`
	TopK      int            `json:"top_k"`
	Threshold float64        `json:"threshold"`
	Embedding []float32      `json:"embedding"`
}

func (h *Handler) searchMemory(w http.ResponseWriter, r *http.Request) {
	var req searchMemoryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	f := parseFilters(req.Filters)
	results, err := h.svc().Search(r.Context(), SearchInput{
		Query: req.Query, Filters: f, TopK: req.TopK,
		Threshold: req.Threshold, Embedding: req.Embedding,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]agentMemory, 0, len(results))
	for _, sm := range results {
		out = append(out, toAgentMemory(sm.LongEntry, sm.Score))
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := LongFilter{
		AgentID: q.Get("agent_id"),
		UserID:  q.Get("user_id"),
		RunID:   firstNonEmpty(q.Get("run_id"), q.Get("session_id")),
		Kind:    LongKind(q.Get("kind")),
	}
	if f.AgentID == "" && f.UserID == "" && f.RunID == "" {
		writeErr(w, http.StatusBadRequest, "agent_id, user_id, or run_id required")
		return
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	if v := q.Get("page_size"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	if v := q.Get("offset"); v != "" {
		f.Offset, _ = strconv.Atoi(v)
	}
	page := 1
	if v := q.Get("page"); v != "" {
		page, _ = strconv.Atoi(v)
		if page < 1 {
			page = 1
		}
		if f.Limit <= 0 {
			f.Limit = 50
		}
		f.Offset = (page - 1) * f.Limit
	}
	entries, err := h.Store.ListLong(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	count, _ := h.Store.CountLong(r.Context(), f)
	results := make([]agentMemory, 0, len(entries))
	for _, e := range entries {
		results = append(results, toAgentMemory(e, 0))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   count,
		"results": results,
	})
}

func (h *Handler) getMemory(w http.ResponseWriter, r *http.Request) {
	e, err := h.Store.GetLong(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAgentMemory(e, 0))
}

type updateMemoryReq struct {
	Memory     *string           `json:"memory"`
	Text       *string           `json:"text"`
	Kind       *LongKind         `json:"kind"`
	Metadata   map[string]string `json:"metadata"`
	Importance *int              `json:"importance"`
	ExpiresAt  *time.Time        `json:"expiration_date"`
	UserID     *string           `json:"user_id"`
	RunID      *string           `json:"run_id"`
	Embedding  []float32         `json:"embedding"`
}

func (h *Handler) updateMemory(w http.ResponseWriter, r *http.Request) {
	var req updateMemoryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	content := req.Text
	if content == nil {
		content = req.Memory
	}
	e, err := h.svc().Update(r.Context(), r.PathValue("id"), UpdateInput{
		Content: content, Kind: req.Kind, Metadata: req.Metadata,
		Importance: req.Importance, ExpiresAt: req.ExpiresAt,
		UserID: req.UserID, RunID: req.RunID, Embedding: req.Embedding,
	})
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAgentMemory(e, 0))
}

func (h *Handler) deleteMemory(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteLong(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentMemory is the mem0-shaped response object.
type agentMemory struct {
	ID             string            `json:"id"`
	Memory         string            `json:"memory"`
	AgentID        string            `json:"agent_id,omitempty"`
	UserID         string            `json:"user_id,omitempty"`
	RunID          string            `json:"run_id,omitempty"`
	Score          float64           `json:"score,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Categories     []string          `json:"categories,omitempty"`
	Importance     int               `json:"importance,omitempty"`
	ExpirationDate *time.Time        `json:"expiration_date,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func toAgentMemory(e LongEntry, score float64) agentMemory {
	cats := []string{}
	if e.Kind != "" {
		cats = []string{string(e.Kind)}
	}
	return agentMemory{
		ID: e.ID, Memory: e.Content, AgentID: e.AgentID, UserID: e.UserID,
		RunID: e.SourceSessionID, Score: score, Metadata: e.Metadata,
		Categories: cats, Importance: e.Importance, ExpirationDate: e.ExpiresAt,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func parseFilters(m map[string]any) SearchFilter {
	var f SearchFilter
	if m == nil {
		return f
	}
	f.AgentID = filterString(m, "agent_id")
	f.UserID = filterString(m, "user_id")
	f.RunID = filterString(m, "run_id")
	if f.RunID == "" {
		f.RunID = filterString(m, "session_id")
	}
	if k := filterString(m, "kind"); k != "" {
		f.Kind = LongKind(k)
	}
	return f
}

func filterString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// RunPurge periodically deletes expired short-term rows and low-value long-term rows.
func RunPurge(ctx context.Context, store Store, logger *slog.Logger, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			shortN, err := store.DeleteExpiredShort(ctx, now)
			if err != nil {
				logger.Error("memory purge short", slog.Any("err", err))
			}
			longN, err := store.PurgeExpiredLong(ctx, now)
			if err != nil {
				logger.Error("memory purge long", slog.Any("err", err))
			}
			if shortN > 0 || longN > 0 {
				logger.Info("memory purged", slog.Int64("short", shortN), slog.Int64("long", longN))
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
