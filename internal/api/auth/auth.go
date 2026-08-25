// Package auth provides simple API key middleware for roundpend.
package auth

import (
	"net/http"
	"strings"
)

// APIKey middleware requires X-API-KEY when key is non-empty.
func APIKey(key string, next http.Handler) http.Handler {
	if key == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public health / ready probes, and LLM gateway relay (virtual-key auth).
		if r.URL.Path == "/health" || r.URL.Path == "/v1/ready" ||
			strings.HasPrefix(r.URL.Path, "/llmgw/") {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-API-KEY")
		if got == "" {
			got = r.Header.Get("X-API-Key")
		}
		if got == "" {
			if a := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(a), "bearer ") {
				got = strings.TrimSpace(a[7:])
			}
		}
		if got != key {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
