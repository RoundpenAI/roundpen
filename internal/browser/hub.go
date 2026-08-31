package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Hub owns per-sandbox Chrome sessions (kern sidecar / host CDP).
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*Session
	dataDir  string
	logger   *slog.Logger

	newEngine func(userDataDir string, width, height int) (Engine, error)
}

// Session is one attached browser.
type Session struct {
	SandboxID string
	Engine    Engine
	Width     int
	Height    int
}

// NewHub returns a session hub. dataDir holds Chrome user-data dirs.
func NewHub(dataDir string, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		sessions: map[string]*Session{},
		dataDir:  dataDir,
		logger:   logger,
		newEngine: func(dir string, w, h int) (Engine, error) {
			return newChromeEngine(dir, w, h)
		},
	}
}

// CloseSandbox tears down the Chrome process for a sandbox (Stop/Delete).
func (h *Hub) CloseSandbox(id string) {
	if h == nil || id == "" {
		return
	}
	h.mu.Lock()
	sess := h.sessions[id]
	delete(h.sessions, id)
	h.mu.Unlock()
	if sess != nil && sess.Engine != nil {
		_ = sess.Engine.Close()
	}
}

// Close releases every session.
func (h *Hub) Close() {
	h.mu.Lock()
	ids := make([]string, 0, len(h.sessions))
	for id := range h.sessions {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.CloseSandbox(id)
	}
}

// Status reports whether a session is attached, without starting Chrome.
func (h *Hub) Status(id string) (attached bool, url string, width, height int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sess := h.sessions[id]
	if sess == nil || sess.Engine == nil {
		return false, "", 0, 0
	}
	return true, sess.Engine.URL(), sess.Width, sess.Height
}

// Ensure starts Chrome for the sandbox if needed.
func (h *Hub) Ensure(ctx context.Context, id string) (*Session, error) {
	_ = ctx
	if id == "" {
		return nil, fmt.Errorf("sandbox id is required")
	}
	h.mu.Lock()
	if sess := h.sessions[id]; sess != nil && sess.Engine != nil {
		h.mu.Unlock()
		return sess, nil
	}
	h.mu.Unlock()

	dir := filepath.Join(h.dataDir, "browser", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	eng, err := h.newEngine(dir, 1280, 800)
	if err != nil {
		return nil, err
	}
	sess := &Session{SandboxID: id, Engine: eng, Width: 1280, Height: 800}
	h.mu.Lock()
	if existing := h.sessions[id]; existing != nil && existing.Engine != nil {
		h.mu.Unlock()
		_ = eng.Close()
		return existing, nil
	}
	h.sessions[id] = sess
	h.mu.Unlock()
	h.logger.Info("browser session started", slog.String("sandbox", id))
	return sess, nil
}
