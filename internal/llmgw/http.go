package llmgw

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Mount registers relay and admin routes on mux.
//
// Relay (virtual-key auth; exempt from control-plane API key):
//
//	/llmgw/anthropic/...
//	/llmgw/openai/...
//
// Admin (control-plane API key):
//
//	GET /v1/llmgw/virtual-keys
//	GET /v1/llmgw/logs
//	GET /v1/llmgw/logs/{id}
//	GET /v1/llmgw/stats
//	GET /v1/llmgw/setup
func (g *Gateway) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/llmgw/anthropic/", g.serveAnthropic)
	mux.HandleFunc("/llmgw/openai/", g.serveOpenAI)

	mux.HandleFunc("GET /v1/llmgw/virtual-keys", auth.RequireAdmin(g.handleVirtualKeys))
	mux.HandleFunc("GET /v1/llmgw/logs", auth.RequireAdmin(g.handleLogs))
	mux.HandleFunc("GET /v1/llmgw/logs/{id}", auth.RequireAdmin(g.handleLogDetail))
	mux.HandleFunc("GET /v1/llmgw/stats", auth.RequireAdmin(g.handleStats))
	mux.HandleFunc("GET /v1/llmgw/setup", auth.RequireAdmin(g.handleSetup))
}

func (g *Gateway) handleVirtualKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := g.store.ListVirtualKeys(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	type keyInfo struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	out := make([]keyInfo, 0, len(keys))
	for _, vk := range keys {
		out = append(out, keyInfo{Key: maskVirtualKey(vk.Key), Name: vk.Name, Enabled: vk.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *Gateway) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	logs, err := g.store.ListTransactions(r.Context(), ListOptions{
		VirtualKey: q.Get("virtual_key"),
		Provider:   q.Get("provider"),
		Limit:      parseIntDefault(q.Get("limit"), 100),
		Offset:     parseIntDefault(q.Get("offset"), 0),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

type logDetail struct {
	Transaction
	RequestBody              string `json:"request_body,omitempty"`
	ResponseBody             string `json:"response_body,omitempty"`
	UpstreamRequestBody      string `json:"upstream_request_body,omitempty"`
	RequestTruncated         bool   `json:"request_truncated,omitempty"`
	ResponseTruncated        bool   `json:"response_truncated,omitempty"`
	UpstreamRequestTruncated bool   `json:"upstream_request_truncated,omitempty"`
}

func (g *Gateway) handleLogDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	tx, bodies, err := g.store.GetTransaction(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	detail := logDetail{Transaction: tx}
	if bodies != nil {
		detail.RequestBody = bodies.Request
		detail.ResponseBody = bodies.Response
		detail.UpstreamRequestBody = bodies.UpstreamRequest
		detail.RequestTruncated = bodies.RequestTruncated
		detail.ResponseTruncated = bodies.ResponseTruncated
		detail.UpstreamRequestTruncated = bodies.UpstreamRequestTruncated
	}
	writeJSON(w, http.StatusOK, detail)
}

func (g *Gateway) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := g.store.Stats(r.Context(), r.URL.Query().Get("virtual_key"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type setupProvider struct {
	Enabled    bool   `json:"enabled"`
	PathPrefix string `json:"path_prefix"`
	BaseURL    string `json:"base_url,omitempty"`
}

type setupModel struct {
	Alias  string `json:"alias"`
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
	Regex  bool   `json:"regex,omitempty"`
}

type setupResponse struct {
	BaseURL          string                   `json:"base_url"`
	Providers        map[string]setupProvider `json:"providers"`
	VirtualKeys      []VirtualKey             `json:"virtual_keys"`
	DownstreamModels map[string][]setupModel  `json:"downstream_models"`
}

func (g *Gateway) handleSetup(w http.ResponseWriter, r *http.Request) {
	upstreams, err := g.store.ListUpstreams(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	keys, err := g.store.ListVirtualKeys(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	masked := make([]VirtualKey, 0, len(keys))
	for _, vk := range keys {
		vk.Key = maskVirtualKey(vk.Key)
		masked = append(masked, vk)
	}
	out := setupResponse{
		BaseURL:   publicBaseURL(r, g.publicBase()),
		Providers: map[string]setupProvider{},
		DownstreamModels: map[string][]setupModel{
			ProviderAnthropic: {},
			ProviderOpenAI:    {},
		},
		VirtualKeys: masked,
	}

	for _, u := range upstreams {
		if !u.Enabled {
			continue
		}
		out.Providers[u.Provider] = setupProvider{
			Enabled:    true,
			PathPrefix: "/llmgw/" + u.Provider,
			BaseURL:    u.BaseURL,
		}
		out.DownstreamModels[u.Provider] = downstreamModels(u)
	}
	writeJSON(w, http.StatusOK, out)
}

func downstreamModels(u Upstream) []setupModel {
	models := make([]setupModel, 0, len(u.ModelMap)+len(u.ModelPatterns))
	for alias, target := range u.ModelMap {
		models = append(models, setupModel{Alias: alias, Type: "exact", Target: target})
	}
	for _, p := range u.ModelPatterns {
		typ := "pattern"
		if p.Regex {
			typ = "regex"
		}
		models = append(models, setupModel{
			Alias:  p.Pattern,
			Type:   typ,
			Target: p.Target,
			Regex:  p.Regex,
		})
	}
	return models
}

func publicBaseURL(r *http.Request, configured string) string {
	if u := strings.TrimRight(strings.TrimSpace(configured), "/"); u != "" {
		return u
	}
	return httpx.DefaultTrust.Scheme(r) + "://" + httpx.DefaultTrust.Host(r)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
