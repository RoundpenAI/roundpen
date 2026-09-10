// Command uismoke serves a real envapi + RFB desktop for Playwright UI smoke.
// It uses the production desktop WebSocket proxy (token → Unix RFB), not a fake URL.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/envapi"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/rfbtest"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19021", "HTTP listen address")
	public := flag.String("public", "http://127.0.0.1:4173", "UI origin used in desktop wsUrl")
	flag.Parse()

	sock := filepath.Join(os.TempDir(), "roundpen-uismoke-vnc.sock")
	ln, err := rfbtest.ListenUnix(sock)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	defer os.Remove(sock)

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

	envs := &slotEnvs{}
	mux := http.NewServeMux()
	auth.Mount(mux, users, sessions, func() bool { return false })
	(&envapi.Handler{
		Envs:      envs,
		Tokens:    preview.NewStore(0),
		PublicURL: *public,
		VNC:       vncSock(sock),
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
			"names": []string{"browser-desktop"}, "aliases": []string{"browser-desktop"},
			"buildStatus": "ready", "envdVersion": "",
			"createdAt": "2026-01-01T00:00:00Z",
		}})
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
				"kanikoDestination":       "",
				"kanikoExecutor":          "",
				"kanikoRegistryMirrors":   "",
				"kanikoInsecure":          false,
				"kanikoSkipTlsVerify":     false,
				"kanikoExtraArgs":         "",
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

	log.Printf("uismoke-api %s public=%s vnc=%s", *listen, *public, sock)
	if err := http.ListenAndServe(*listen, auth.Middleware(users, sessions)(mux)); err != nil {
		log.Fatal(err)
	}
}

type vncSock string

func (s vncSock) VNCSock(string) (string, error) { return string(s), nil }

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
	browser := userenv.EnvView{Slot: userenv.SlotBrowser, Status: "absent", TemplateID: "browser-desktop"}
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

func (s *slotEnvs) EnsureBrowser(_ context.Context, _ string) (*sandbox.Sandbox, error) {
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
	return s.browser, nil
}

func (s *slotEnvs) EnsureAgent(_ context.Context, _ string) (*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("agent slot not used in uismoke")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
