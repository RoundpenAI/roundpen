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
	h := auth.Middleware(users, sessions)(mux)
	h = gatewayIdentity(users, sessions)(h)
	return stripGatewayPrefix(prefix, h)
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
	srv := &http.Server{
		Handler:           Handler(prefix, users, sessions, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // terminal WS streams past a write deadline
		IdleTimeout:       120 * time.Second,
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
