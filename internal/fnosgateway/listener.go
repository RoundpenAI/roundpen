//go:build fnos

package fnosgateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Handler assembles the fnOS Unix-socket handler chain:
//
//	stripGatewayPrefix(prefix) → gatewayIdentity(users, sessions) → auth.Middleware(users, sessions)(mux)
//
// The prefix is stripped first so the downstream mux and identity middleware
// see the same paths they would on the TCP listener.
func Handler(prefix string, users storage.UserStore, sessions storage.SessionStore, mux http.Handler) http.Handler {
	return HandlerWithLogger(prefix, users, sessions, mux, nil)
}

// HandlerWithLogger is Handler with an optional slog logger for upgrade probes.
func HandlerWithLogger(prefix string, users storage.UserStore, sessions storage.SessionStore, mux http.Handler, logger *slog.Logger) http.Handler {
	h := auth.Middleware(users, sessions)(mux)
	h = gatewayIdentity(users, sessions)(h)
	h = stripGatewayPrefix(prefix, h)
	return logGatewayUpgrades(logger, h)
}

// stripGatewayPrefix removes the gateway path prefix from the request the fnOS
// gateway forwards. It is tolerant: when the prefix is absent the request is
// passed through unchanged, and an empty remainder is normalised to "/".
func stripGatewayPrefix(prefix string, next http.Handler) http.Handler {
	prefix = strings.TrimRight(prefix, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if prefix != "" {
			if p, ok := strings.CutPrefix(r.URL.Path, prefix); ok {
				r.URL.Path = normalizeGatewayPath(p)
			}
			if r.URL.RawPath != "" {
				if rp, ok := strings.CutPrefix(r.URL.RawPath, prefix); ok {
					r.URL.RawPath = normalizeGatewayPath(rp)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// logGatewayUpgrades logs WebSocket upgrade attempts that reach the Unix
// socket. If the desktop shows a WS failure but this line never appears, the
// fnOS gateway (or the desktop proxy on :5666) is not forwarding the Upgrade.
func logGatewayUpgrades(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connHdr := strings.ToLower(r.Header.Get("Connection"))
		if strings.Contains(connHdr, "upgrade") ||
			strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			logger.Info("fnos gateway websocket upgrade",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("origin", r.Header.Get("Origin")),
				slog.String("host", r.Host),
				slog.String("x_forwarded_host", r.Header.Get("X-Forwarded-Host")),
				slog.Bool("has_trim_userid", r.Header.Get("X-Trim-Userid") != ""),
				slog.String("remote", r.RemoteAddr),
			)
		}
		next.ServeHTTP(w, r)
	})
}

func normalizeGatewayPath(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

// TCPHandler mounts the console at the gateway prefix on the TCP listener too.
// The fnOS SPA is built for the "/app/roundpen/" base, so prefixed requests are
// unwrapped and a bare "/" is redirected to the prefixed console so the SPA
// router basename matches the URL.
func TCPHandler(prefix string, next http.Handler) http.Handler {
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return next
	}
	inner := stripGatewayPrefix(prefix, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			(r.URL.Path == "/" || r.URL.Path == "/index.html") {
			http.Redirect(w, r, prefix+"/", http.StatusFound)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// Serve listens on the fnOS gateway Unix socket and serves the gateway handler
// chain. A stale socket left by a previous run is removed first. The returned
// io.Closer shuts the server down.
func Serve(socket, prefix string, users storage.UserStore, sessions storage.SessionStore, mux http.Handler, logger *slog.Logger) (io.Closer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("fnos gateway: remove stale socket %s: %w", socket, err)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("fnos gateway: listen unix %s: %w", socket, err)
	}
	// Nginx (unified gateway) connects as a different uid; world-writable socket
	// is the usual pattern for app.sock under target/.
	if err := os.Chmod(socket, 0o666); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("fnos gateway: chmod %s: %w", socket, err)
	}
	srv := &http.Server{
		Handler:           HandlerWithLogger(prefix, users, sessions, mux, logger),
		ReadHeaderTimeout: 10 * time.Second,
		// No ReadTimeout: hijacked WebSocket connections reuse the accepted
		// conn; a global read deadline would tear down long-lived chat sockets.
		WriteTimeout: 0, // terminal WS streams past a write deadline
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("fnos gateway socket server", slog.Any("err", err), slog.String("socket", socket))
		}
	}()
	logger.Info("fnos gateway listening", slog.String("socket", socket), slog.String("prefix", prefix))
	return &socketServer{srv: srv, ln: ln}, nil
}

// socketServer is the io.Closer for a running gateway listener.
type socketServer struct {
	srv *http.Server
	ln  net.Listener
}

func (s *socketServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.srv.Shutdown(ctx)
	_ = s.ln.Close()
	return err
}
