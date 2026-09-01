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

// Handler returns an http.Handler that serves the embedded UI.
// Unknown paths fall back to index.html for client-side routing.
func Handler() http.Handler {
	fsys, err := fs.Sub(distEmbed, "dist")
	if err != nil {
		panic("ui: dist subtree: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}
