package otelx

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// WrapHandler adds otelhttp tracing around next. No-op wrapper when OTEL is off
// still works (otelhttp uses the global no-op provider).
func WrapHandler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "roundpend",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return !isNoisePath(r.URL.Path)
		}),
	)
}

func isNoisePath(path string) bool {
	switch path {
	case "/health", "/v1/ready":
		return true
	}
	// Embedded SPA assets and root shell — high volume, low signal.
	if path == "/" || path == "/index.html" {
		return true
	}
	if strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/static/") {
		return true
	}
	// Vite / favicon noise on console.
	if path == "/favicon.ico" || path == "/manifest.webmanifest" || path == "/sw.js" {
		return true
	}
	return false
}
