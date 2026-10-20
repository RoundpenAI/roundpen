//go:build fnos

package main

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/fnosgateway"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// installFnosGateway starts the fnOS unified-gateway Unix-socket listener when
// ROUNDPEN_FNOS_GATEWAY_SOCKET is set. It returns (nil, nil) when the gateway is
// not configured, so a non-fnOS deployment that happens to build with -tags
// fnos stays inert.
func installFnosGateway(cfg *config.Config, users storage.UserStore, sessions storage.SessionStore, mux *http.ServeMux, logger *slog.Logger) (io.Closer, error) {
	socket := strings.TrimSpace(cfg.FnosGatewaySocket)
	if socket == "" {
		return nil, nil
	}
	closer, err := fnosgateway.Serve(socket, cfg.FnosGatewayPrefix, users, sessions, mux, logger)
	if err != nil {
		return nil, err
	}
	auth.SetFnosGatewayActive(true)
	// SPA is built with VITE_BASE=/app/roundpen/; browser redirects must match.
	httpx.SetUIBase(cfg.FnosGatewayPrefix)
	return closer, nil
}

// fnosConsoleWrapper mounts the TCP console at the gateway prefix when the
// gateway is active, so the port-based console and the gateway console resolve
// to the same URL. Inert when the gateway is not configured.
func fnosConsoleWrapper(cfg *config.Config, next http.Handler) http.Handler {
	if strings.TrimSpace(cfg.FnosGatewaySocket) == "" || strings.TrimSpace(cfg.FnosGatewayPrefix) == "" {
		return next
	}
	return fnosgateway.TCPHandler(cfg.FnosGatewayPrefix, next)
}
