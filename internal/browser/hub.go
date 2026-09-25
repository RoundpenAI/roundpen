package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
)

const cdpReadyWait = 45 * time.Second

// Hub owns per-sandbox browser sessions attached via a CDP provider.
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*Session
	dataDir  string
	logger   *slog.Logger

	profiles map[string]Profile // hub key → browser source
	cfg      *config.Config
	dial     PortDialer
	driver   *pwDriver
	// tokenLookup resolves a sandbox id to its stored browser token
	// (sandbox.Metadata["browserToken"]); wired in cmd/roundpend.
	tokenLookup func(sandboxID string) string
	// profileResolver resolves a hub key to the user's configured browser
	// profile from the settings catalog. Falls back to DefaultProfile(cfg)
	// when nil or when the resolver returns ok=false.
	profileResolver func(key string) (Profile, bool)

	newEngine func(userDataDir string, width, height int) (Engine, error)
}

// Session is one attached browser.
type Session struct {
	SandboxID string
	Engine    Engine
	Width     int
	Height    int
	Takeover  bool
	release   func()
}

// NewHub returns a session hub. dataDir holds per-session scratch dirs for the
// injected test engine; production providers keep their state elsewhere.
func NewHub(dataDir string, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		sessions: map[string]*Session{},
		dataDir:  dataDir,
		logger:   logger,
		driver:   &pwDriver{},
	}
}

// SetConfig supplies live process config (provider can change via settings apply).
func (h *Hub) SetConfig(cfg *config.Config) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()
}

// SetTokenLookup supplies sandbox metadata lookup for the browserless token.
// The callback runs outside the hub lock, so it is safe for it to touch
// sandbox or hub state.
func (h *Hub) SetTokenLookup(f func(sandboxID string) string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.tokenLookup = f
	h.mu.Unlock()
}

// SetProfileResolver supplies a callback that resolves a hub key to the user's
// configured browser profile from the settings catalog. When set, attach uses
// it before falling back to the process config, so a user-configured remote or
// cloud endpoint is never silently overridden by the docker default.
func (h *Hub) SetProfileResolver(f func(key string) (Profile, bool)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.profileResolver = f
	h.mu.Unlock()
}

// browserToken returns the browserless token stored for a sandbox; the lookup
// runs outside h.mu so it cannot deadlock against other Hub methods.
func (h *Hub) browserToken(sandboxID string) string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	lookup := h.tokenLookup
	h.mu.Unlock()
	if lookup == nil {
		return ""
	}
	return lookup(sandboxID)
}

// SetProfile records the browser source a hub key attaches to. The control
// plane calls it whenever it resolves a user's browser environment; a key with
// no record falls back to the process config.
func (h *Hub) SetProfile(key string, profile Profile) {
	h.mu.Lock()
	if h.profiles == nil {
		h.profiles = map[string]Profile{}
	}
	h.profiles[key] = profile
	h.mu.Unlock()
}

// ForgetProfile drops a key's recorded source (sandbox teardown).
func (h *Hub) ForgetProfile(key string) {
	h.mu.Lock()
	delete(h.profiles, key)
	h.mu.Unlock()
}

// SetDialer supplies sandbox port dialing for the docker provider.
func (h *Hub) SetDialer(d PortDialer) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.dial = d
	h.mu.Unlock()
}

// CloseSandbox tears down the session for a sandbox (Stop/Delete).
func (h *Hub) CloseSandbox(id string) {
	if h == nil || id == "" {
		return
	}
	h.ForgetProfile(id)
	h.mu.Lock()
	sess := h.sessions[id]
	delete(h.sessions, id)
	h.mu.Unlock()
	if sess == nil {
		return
	}
	if sess.release != nil {
		sess.release()
	}
	if sess.Engine != nil {
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
	h.driver.stop()
}

// Status reports whether a session is attached, without starting a browser.
func (h *Hub) Status(id string) (attached bool, url string, width, height int) {
	st := h.StatusEx(id)
	return st.Attached, st.URL, st.Width, st.Height
}

// SessionStatus is a non-mutating view of a Hub session.
type SessionStatus struct {
	Attached bool
	URL      string
	Width    int
	Height   int
	Takeover bool
}

// StatusEx reports attachment and takeover without starting a browser.
func (h *Hub) StatusEx(id string) SessionStatus {
	if h == nil {
		return SessionStatus{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	sess := h.sessions[id]
	if sess == nil || sess.Engine == nil {
		return SessionStatus{}
	}
	return SessionStatus{
		Attached: true,
		URL:      sess.Engine.URL(),
		Width:    sess.Width,
		Height:   sess.Height,
		Takeover: sess.Takeover,
	}
}

// Takeover reports whether the session is under human control.
func (h *Hub) Takeover(id string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	sess := h.sessions[id]
	return sess != nil && sess.Takeover
}

// SetTakeover enables or disables human takeover for an existing session.
// If the session is not attached yet, Ensure is required first when enabling.
func (h *Hub) SetTakeover(id string, on bool) error {
	if h == nil {
		return fmt.Errorf("browser hub not configured")
	}
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	sess := h.sessions[id]
	if sess == nil {
		if !on {
			return nil
		}
		return fmt.Errorf("browser session not attached")
	}
	sess.Takeover = on
	return nil
}

// Ensure attaches a CDP session for the sandbox if needed.
func (h *Hub) Ensure(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("sandbox id is required")
	}
	h.mu.Lock()
	if sess := h.sessions[id]; sess != nil && sess.Engine != nil {
		h.mu.Unlock()
		return sess, nil
	}
	h.mu.Unlock()

	sess, err := h.attachReady(ctx, id)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if existing := h.sessions[id]; existing != nil && existing.Engine != nil {
		h.mu.Unlock()
		if sess.release != nil {
			sess.release()
		}
		if sess.Engine != nil {
			_ = sess.Engine.Close()
		}
		return existing, nil
	}
	h.sessions[id] = sess
	h.mu.Unlock()
	h.logger.Info("browser session started", slog.String("sandbox", id))
	return sess, nil
}

func (h *Hub) attachReady(ctx context.Context, id string) (*Session, error) {
	if h.newEngine != nil {
		return h.attach(ctx, id)
	}
	wait := cdpReadyWait
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline); remain > 0 && remain < wait {
			wait = remain
		}
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	var last error
	for {
		sess, err := h.attach(ctx, id)
		if err == nil {
			return sess, nil
		}
		last = err
		if !cdpRetryable(err) {
			return nil, err
		}
		if h.logger != nil {
			h.logger.Info("waiting for guest chrome CDP", slog.String("sandbox", id), slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return nil, last
		case <-time.After(time.Second):
		}
	}
}

func cdpRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "requires a sandbox dialer") {
		return false
	}
	for _, needle := range []string{
		"cdp attach",
		"econnrefused",
		"connection refused",
		"connection reset",
		"websocket was closed before the connection was established",
		"socket hang up",
		"timeout",
		"timed out",
		"empty reply",
		"eof",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func (h *Hub) attach(ctx context.Context, id string) (*Session, error) {
	if h.newEngine != nil { // test injection, unchanged
		dir := filepath.Join(h.dataDir, "browser", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		eng, err := h.newEngine(dir, 1280, 800)
		if err != nil {
			return nil, err
		}
		return &Session{SandboxID: id, Engine: eng, Width: 1280, Height: 800}, nil
	}

	h.mu.Lock()
	cfg := h.cfg
	dial := h.dial
	driver := h.driver
	profile, recorded := h.profiles[id]
	h.mu.Unlock()

	if driver == nil {
		driver = &pwDriver{}
	}

	width, height := 1280, 800
	if !recorded {
		h.mu.Lock()
		resolver := h.profileResolver
		h.mu.Unlock()
		if resolver != nil {
			profile, recorded = resolver(id)
		}
		if !recorded {
			profile = DefaultProfile(cfg)
		}
	}
	provider := profile.EffectiveProvider()
	// att starts token-less: host/remote/cloud may only pass the endpoint token
	// from config; only the docker provider reads the sandbox's own token.
	att := pwAttach{Width: width, Height: height}

	switch provider {
	case config.CDPProviderHost:
		if strings.TrimSpace(profile.Endpoint) != "" {
			att.Endpoint = profile.Endpoint
			if profile.Token != "" {
				att.Token = profile.Token
			}
			eng, err := newPlaywrightEngine(driver, att)
			if err != nil {
				return nil, fmt.Errorf("host cdp: %w", err)
			}
			return &Session{SandboxID: id, Engine: eng, Width: width, Height: height}, nil
		}
		bin := lookupChrome()
		if bin == "" {
			return nil, fmt.Errorf("host chrome not found (set CHROME_PATH)")
		}
		att.LaunchPath = bin
		eng, err := newPlaywrightEngine(driver, att)
		if err != nil {
			return nil, fmt.Errorf("host chrome: %w", err)
		}
		return &Session{SandboxID: id, Engine: eng, Width: width, Height: height}, nil

	case config.CDPProviderRemote, config.CDPProviderCloud:
		att.Endpoint = profile.Endpoint
		att.Token = profile.Token
		eng, err := newPlaywrightEngine(driver, att)
		if err != nil {
			return nil, fmt.Errorf("%s cdp: %w", provider, err)
		}
		return &Session{SandboxID: id, Engine: eng, Width: width, Height: height}, nil

	case config.CDPProviderDocker:
		port := profile.Port
		if port <= 0 {
			port = config.DefaultCDPPort
		}
		if dial == nil {
			return nil, fmt.Errorf("docker cdp requires a sandbox dialer")
		}
		localURL, stop, err := startCDPProxy(dial, id, port)
		if err != nil {
			return nil, fmt.Errorf("env cdp: %w", err)
		}
		att.Endpoint = localURL
		att.Token = h.browserToken(id)
		eng, err := newPlaywrightEngine(driver, att)
		if err != nil {
			stop()
			return nil, fmt.Errorf("env cdp (Browser env :%d not serving CDP): %w", port, err)
		}
		return &Session{SandboxID: id, Engine: eng, Width: width, Height: height, release: stop}, nil
	default:
		return nil, fmt.Errorf("unknown cdp provider %q", provider)
	}
}
