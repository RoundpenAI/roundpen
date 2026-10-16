//go:build !fnos

package main

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// installFnosGateway is the default-build stub: the fnOS gateway is compiled out
// (identity.go / listener.go carry the fnos build tag), so there is nothing to
// start. The signature matches the fnos build exactly.
func installFnosGateway(_ *config.Config, _ storage.UserStore, _ storage.SessionStore, _ *http.ServeMux, _ *slog.Logger) (io.Closer, error) {
	return nil, nil
}

// fnosConsoleWrapper is a no-op without the fnos build tag: the console is
// served at "/" for every other channel.
func fnosConsoleWrapper(_ *config.Config, next http.Handler) http.Handler {
	return next
}
