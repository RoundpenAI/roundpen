// Command uismoke serves a real envapi plus a stub browserless debugger
// upstream for Playwright UI smoke. The live view goes through the production
// proxy (token → sandbox debugger port), not a fake URL.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/envapi"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19021", "HTTP listen address")
	flag.Parse()

	hash, err := auth.HashPassword("adminadmin")
	if err != nil {
		log.Fatal(err)
	}
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(context.Background(), storage.User{
		Username:     "admin",
		Email:        "admin@roundpen.test",
		FullName:     "Admin",
		OrgName:      "Roundpen",
		APIKey:       "rp-uismoke",
		Role:         storage.RoleAdmin,
		PasswordHash: hash,
	}); err != nil {
		log.Fatal(err)
	}
	sessions := storage.NewMemorySessionStore()

	// Stub upstream for the browser live view: the proxy dials this instead of
	// a sandbox's browserless debugger port. A WebSocket upgrade gets an
	// immediate 426: replying with a body would leave the live proxy's
	// bidirectional copy blocked on an idle keep-alive connection.
	liveUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no websocket here", http.StatusUpgradeRequired)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_, _ = io.WriteString(conn, "HTTP/1.1 426 Upgrade Required\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>browserless debugger</title><div id="debugger-ui">uismoke live stub</div>`)
	}))
	defer liveUpstream.Close()

	envs := &slotEnvs{}
	mux := http.NewServeMux()
	auth.Mount(mux, users, sessions, func() bool { return false })
	(&envapi.Handler{
		Envs: envs,
		Dial: liveDialer(strings.TrimPrefix(liveUpstream.URL, "http://")),
	}).Mount(mux)
	mux.HandleFunc("GET /v1/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("GET /v1/agents", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"agents": []map[string]any{{
				"id": "sysadmin", "name": "Sysadmin", "enabled": true, "mode": "local",
			}},
		})
	})
	mux.HandleFunc("GET /v1/agent-sessions", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"sessions": []any{}})
	})
	mux.HandleFunc("GET /v1/browser-tasks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"tasks": []any{}})
	})
	mux.HandleFunc("GET /v1/templates", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{{
			"templateID": "tpl-browser", "buildID": "b1",
			"cpuCount": 2, "memoryMB": 2048, "diskSizeMB": 8192,
			"public": true, "profile": "browser", "slot": "browser",
			"names": []string{"browser"}, "aliases": []string{"browser"},
			"buildStatus": "ready", "envdVersion": "",
			"createdAt": "2026-01-01T00:00:00Z",
		}})
	})
	mux.HandleFunc("GET /v1/setup/llm-ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"ready": true})
	})
	mux.HandleFunc("POST /v1/setup/plans", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writePlan(w, "plan-smoke", body)
	})
	mux.HandleFunc("GET /v1/setup/plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		writePlan(w, r.PathValue("id"), map[string]any{})
	})

	assistants := &assistantStore{}
	mux.HandleFunc("GET /v1/assistants", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"assistants": assistants.list()})
	})
	mux.HandleFunc("POST /v1/assistants", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name         string `json:"name"`
			Bio          string `json:"bio"`
			IdentityMode string `json:"identityMode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, assistants.create(body.Name, body.Bio, body.IdentityMode))
	})
	mux.HandleFunc("POST /v1/assistants/{id}/ensure-session", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"sessionId": "sess-" + r.PathValue("id")})
	})

	mux.HandleFunc("GET /v1/admin/settings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"settings": map[string]any{
				"allowPublicRegistration": false,
				"defaultImage":            "host",
				"defaultTtlSeconds":       1800,
				"previewPublicUrl":        "",
				"previewTokenTtlSeconds":  900,
				"templateBuilder":         "",
				"llmgwEnabled":            false,
				"llmgwPublicUrl":          "",
				"llmgwLogBodyMaxBytes":    -1,
				"llmgwEmbeddingModel":     "text-embedding-3-small",
				"llmgwDefaultModel":       "",
				"llmgwOpenaiBaseUrl":      "",
				"llmgwOpenaiApiKey":       "",
				"llmgwAnthropicBaseUrl":   "",
				"llmgwAnthropicApiKey":    "",
				"llmgwVirtualKeys":        "",
				"cdpProvider":             "auto",
				"cdpEndpoint":             "",
				"cdpToken":                "",
				"cdpPort":                 9222,
			},
			"system": map[string]any{
				"backend":               "qemu",
				"dockerHost":            "",
				"dataRoot":              "/tmp/roundpen",
				"httpAddr":              *listen,
				"templateBuilderActive": "none",
				"llmgwActive":           false,
				"llmgwMounted":          false,
				"cdpProviderActive":     "auto",
				"cdpHostChromeFound":    false,
			},
		})
	})
	mux.HandleFunc("PUT /v1/test/ensure-error", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		envs.setEnsureError(body.Error)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /v1/test/reset", func(w http.ResponseWriter, _ *http.Request) {
		envs.reset()
		writeJSON(w, map[string]any{"ok": true})
	})

	log.Printf("uismoke-api %s live=%s", *listen, liveUpstream.URL)
	if err := http.ListenAndServe(*listen, auth.Middleware(users, sessions)(mux)); err != nil {
		log.Fatal(err)
	}
}

// writePlan returns a completed setup plan: empty actions count as all-terminal,
// so the UI setup workstation fires onReady on its first poll.
func writePlan(w http.ResponseWriter, id string, context map[string]any) {
	if context == nil {
		context = map[string]any{}
	}
	writeJSON(w, map[string]any{
		"id":        id,
		"summary":   "smoke setup plan",
		"context":   context,
		"actions":   []any{},
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	})
}

type assistantStore struct {
	mu    sync.Mutex
	items []map[string]any
	seq   int
}

func (s *assistantStore) list() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		return []map[string]any{}
	}
	return s.items
}

func (s *assistantStore) create(name, bio, identityMode string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if name == "" {
		name = "assistant"
	}
	if identityMode == "" {
		identityMode = "proxy_user"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	a := map[string]any{
		"id":               fmt.Sprintf("as-%d", s.seq),
		"userId":           "admin",
		"name":             name,
		"bio":              bio,
		"identityMode":     identityMode,
		"capabilities":     map[string]any{},
		"networkTier":      "none",
		"networkAllowlist": []any{},
		"directoryGrants":  []any{},
		"status":           "active",
		"kind":             "user",
		"createdAt":        now,
		"updatedAt":        now,
	}
	s.items = append(s.items, a)
	return a
}

// liveDialer dials a fixed TCP address regardless of sandbox id / port.
type liveDialer string

func (d liveDialer) Dial(ctx context.Context, _ string, _ int) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp", string(d))
}

type slotEnvs struct {
	mu        sync.Mutex
	browser   *sandbox.Sandbox
	ensureErr error
}

func (s *slotEnvs) setEnsureError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg == "" {
		s.ensureErr = nil
		return
	}
	s.ensureErr = fmt.Errorf("%s", msg)
}

func (s *slotEnvs) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.browser = nil
	s.ensureErr = nil
}

func (s *slotEnvs) List(_ context.Context, _ string) ([]userenv.EnvView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	browser := userenv.EnvView{Slot: userenv.SlotBrowser, Status: "absent", TemplateID: "browser"}
	if s.browser != nil {
		browser.SandboxID = s.browser.ID
		browser.Status = string(s.browser.Status)
		browser.Name = s.browser.Name
	}
	return []userenv.EnvView{
		{Slot: userenv.SlotAgent, Status: "absent"},
		browser,
		{Slot: userenv.SlotMobile, Status: "reserved"},
	}, nil
}

func (s *slotEnvs) EnsureBrowser(_ context.Context, _ string) (*userenv.BrowserTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ensureErr != nil {
		err := s.ensureErr
		s.ensureErr = nil
		return nil, err
	}
	if s.browser == nil {
		s.browser = &sandbox.Sandbox{
			ID:     "sb-browser",
			Name:   "browser-admin",
			Status: sandbox.StatusRunning,
		}
	}
	return &userenv.BrowserTarget{Key: s.browser.ID, Provider: "docker", Managed: true, Sandbox: s.browser}, nil
}

func (s *slotEnvs) EnsureAgent(_ context.Context, _ string) (*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("agent slot not used in uismoke")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
