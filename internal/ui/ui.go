// Package ui embeds the Roundpen console SPA and serves it from roundpend.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distEmbed embed.FS

// contentSecurityPolicy is the CSP for the console SPA only (mounted at "/").
// 'unsafe-eval' is required because Semi MarkdownRender compiles MDX at
// runtime via new Function; the Google Fonts hosts are referenced from
// index.html. API and preview routes are mounted separately and unaffected.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com data:; " +
	"img-src * data: blob:; " +
	"media-src * blob:; " +
	"connect-src 'self' ws: wss:; " +
	"frame-src 'self' blob:; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// Handler returns an http.Handler that serves the embedded UI.
// Unknown paths fall back to index.html for client-side routing.
func Handler() http.Handler {
	fsys, err := fs.Sub(distEmbed, "dist")
	if err != nil {
		panic("ui: dist subtree: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(fsys))

	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if f, err := fsys.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA client routes (/login, /s/:id, …) — only when UI was built.
		if _, err := fsys.Open("index.html"); err != nil {
			http.Error(w, "ui not built — run: make build-ui", http.StatusServiceUnavailable)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})

	return securityHeaders(spa)
}
