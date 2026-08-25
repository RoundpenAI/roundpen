// Package http serves Roundpen-native control-plane REST endpoints.
package httpapi

import "net/http"

// Handler is the native admin / ops API (distinct from E2B compatibility).
type Handler struct{}

// Mount registers native routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
}
