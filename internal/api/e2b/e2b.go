// Package e2b adapts Roundpen's Sandbox Manager to the E2B-compatible HTTP API.
package e2b

import (
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// Handler serves E2B-compatible routes. Route table filled in Phase 1.
type Handler struct {
	Manager sandbox.Manager
}

// Mount registers E2B routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// TODO: sandboxes CRUD / exec aligned with E2B protocol
}
