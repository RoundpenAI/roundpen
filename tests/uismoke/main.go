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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/envapi"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/settings"
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
	identitySeed := []map[string]any{{
		"id": "ident-1", "providerId": "gitea-git-eaxi-com", "providerKind": "gitea",
		"providerLabel": "Gitea", "providerHost": "git.eaxi.com", "login": "octocat",
		"name": "Octo Cat", "email": "octo@example.test", "scopes": "read:user user:email repo",
		"hasRefreshToken": true, "createdAt": "2026-01-01T00:00:00Z",
	}}
	identities := &identityStore{seed: identitySeed}
	identities.reset()
	mux := http.NewServeMux()
	auth.Mount(mux, users, sessions, func() bool { return false })
	(&envapi.Handler{
		Envs:  envs,
		Users: users,
		Proxies: func() []settings.ProxyProfile {
			return []settings.ProxyProfile{
				{ID: "us", Name: "US egress", Description: "overseas"},
				{ID: "jp", Name: "JP egress"},
			}
		},
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
	mux.HandleFunc("GET /v1/assistants/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		for _, a := range assistants.list() {
			if a["id"] == id {
				writeJSON(w, a)
				return
			}
		}
		http.NotFound(w, r)
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

	seed := newSeedSession()
	mux.HandleFunc("GET /v1/agent-sessions", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"sessions": []map[string]any{seed.session}})
	})
	mux.HandleFunc("GET /v1/agent-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == seed.sessionID() {
			writeJSON(w, seed.session)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("POST /v1/agent-sessions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title       string `json:"title"`
			ProviderID  string `json:"providerId"`
			AssistantID string `json:"assistantId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, seed.create(body.Title, body.ProviderID, body.AssistantID))
	})
	mux.HandleFunc("GET /v1/agent-sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"messages": seed.messages})
	})
	mux.HandleFunc("GET /v1/agent-sessions/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"commands": seed.commands})
	})

	stubSettings := map[string]any{
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
		"autoMode": map[string]any{
			"environment": []string{"$defaults"},
			"allow":       []string{"$defaults"},
			"softDeny":    []string{"$defaults"},
			"hardDeny":    []string{"$defaults"},
			"model":       "",
		},
	}
	stubSystem := map[string]any{
		"backend":               "qemu",
		"dockerHost":            "",
		"dataRoot":              "/tmp/roundpen",
		"httpAddr":              *listen,
		"templateBuilderActive": "none",
		"llmgwActive":           false,
		"llmgwMounted":          false,
		"cdpProviderActive":     "auto",
		"cdpHostChromeFound":    false,
	}
	mux.HandleFunc("GET /v1/admin/settings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"settings": stubSettings, "system": stubSystem})
	})
	mux.HandleFunc("PUT /v1/admin/settings", func(w http.ResponseWriter, r *http.Request) {
		var next map[string]any
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		stubSettings = next
		writeJSON(w, map[string]any{"settings": stubSettings, "system": stubSystem})
	})
	mux.HandleFunc("GET /v1/admin/settings/automode/defaults", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"environment": []string{"Trusted environment: the sandbox workspace is normal work."},
			"allow":       []string{"Routine development commands in the sandbox."},
			"softDeny":    []string{"Force-pushing or rewriting remote git history."},
			"hardDeny":    []string{"Reaching cloud metadata or control-plane addresses."},
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
		identities.reset()
		writeJSON(w, map[string]any{"ok": true})
	})

	// Federated login stubs: the login page, the linked-accounts panel and the
	// admin provider form all talk to these.
	mux.HandleFunc("GET /v1/auth/oauth/providers", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"providers": []map[string]any{{
			"id": "gitea-git-eaxi-com", "kind": "gitea", "host": "git.eaxi.com", "label": "Gitea",
		}}})
	})
	mux.HandleFunc("GET /v1/me/identities", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"identities": identities.list()})
	})
	mux.HandleFunc("DELETE /v1/me/identities/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !identities.remove(r.PathValue("id")) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"message": "identity not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/me/identities/link/{provider}", func(w http.ResponseWriter, r *http.Request) {
		// The SPA redirects the browser to this URL; point it back at a stub so
		// the smoke test can assert the navigation without leaving the origin.
		writeJSON(w, map[string]any{
			"authorizeUrl": "/v1/auth/oauth/dev/authorize?provider=" + r.PathValue("provider"),
		})
	})
	// The login page navigates to the real start route; smoke answers it with a
	// redirect to a local stand-in for the remote authorization page.
	mux.HandleFunc("GET /v1/auth/oauth/{provider}/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/auth/oauth/dev/authorize?provider="+url.PathEscape(r.PathValue("provider")), http.StatusFound)
	})
	// Stays under /v1/auth/oauth/ because only that prefix is public to the
	// middleware, exactly like the real remote authorization page.
	mux.HandleFunc("GET /v1/auth/oauth/dev/authorize", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>authorize stub</title><div id="oauth-authorize-stub">authorize stub</div>`)
	})
	mux.HandleFunc("GET /v1/admin/oauth/providers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"providers": []map[string]any{{
			"id": "gitea-git-eaxi-com", "kind": "gitea", "scheme": "https", "host": "git.eaxi.com",
			"label": "Gitea", "clientId": "smoke-client", "clientSecret": "●●●●●●●●",
			"scopes":   "read:user user:email repo",
			"authUrl":  "https://git.eaxi.com/login/oauth/authorize",
			"tokenUrl": "https://git.eaxi.com/login/oauth/access_token",
			"apiUrl":   "https://git.eaxi.com/api/v1", "enabled": true,
			"displayLabel": "Gitea",
			"callbackUrl":  "http://" + r.Host + "/v1/auth/oauth/gitea-git-eaxi-com/callback",
		}}})
	})
	mux.HandleFunc("PUT /v1/admin/oauth/providers", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["displayLabel"] = "Gitea"
		body["callbackUrl"] = "http://" + r.Host + "/v1/auth/oauth/" + fmt.Sprint(body["id"]) + "/callback"
		writeJSON(w, map[string]any{"provider": body})
	})
	mux.HandleFunc("DELETE /v1/admin/oauth/providers/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
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

// identityStore backs the linked-accounts panel so unlink is observable.
type identityStore struct {
	mu    sync.Mutex
	items []map[string]any
	seed  []map[string]any
}

// reset returns the fixture to its seeded state; /v1/test/reset is called
// between specs and must not erase data later specs depend on.
func (s *identityStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make([]map[string]any, len(s.seed))
	copy(s.items, s.seed)
}

func (s *identityStore) list() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, len(s.items))
	copy(out, s.items)
	return out
}

func (s *identityStore) remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item["id"] == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return true
		}
	}
	return false
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

// UpgradeAgent keeps the fake satisfying envapi.Environments; the settings-page
// upgrade button gets a benign answer instead of an error.
func (s *slotEnvs) UpgradeAgent(_ context.Context, _ string, _ bool) (*userenv.UpgradeResult, error) {
	return &userenv.UpgradeResult{
		Status: "up_to_date", Image: "uismoke-agent", Digest: "sha256:uismoke",
		Environment: userenv.EnvView{Slot: userenv.SlotAgent, Status: "running", Image: "uismoke-agent"},
	}, nil
}

// RecreateAgent backs the model-source switch in the fake environment.
func (s *slotEnvs) RecreateAgent(_ context.Context, _ string) (*userenv.UpgradeResult, error) {
	return &userenv.UpgradeResult{Status: "absent"}, nil
}

// RecreateBrowser backs the browser proxy switch in the fake environment.
func (s *slotEnvs) RecreateBrowser(_ context.Context, _ string) (*userenv.UpgradeResult, error) {
	return &userenv.UpgradeResult{Status: "absent"}, nil
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// seedSession fabricates a chat session with a long assistant reply so UI
// smoke can exercise bubble rendering (including narrow mobile viewports)
// without a live agent.
type seedSession struct {
	session  map[string]any
	messages []map[string]any
	commands []map[string]any
}

func (s *seedSession) sessionID() string {
	if s == nil {
		return ""
	}
	_ = s.session
	if id, ok := s.session["id"].(string); ok {
		return id
	}
	return ""
}

func (s *seedSession) create(title, providerID, assistantID string) map[string]any {
	s.session["title"] = title
	if providerID != "" {
		s.session["providerId"] = providerID
	}
	if assistantID != "" {
		s.session["assistantId"] = assistantID
	}
	return s.session
}

func newSeedSession() *seedSession {
	long := "supercalifragilisticexpialidocious "
	long += "pneumonoultramicroscopicsilicovolcanoconiosis "
	long += "antidisestablishmentarianism"
	now := time.Now().UTC().Format(time.RFC3339)
	sess := map[string]any{
		"id":          "sess-seed",
		"userId":      "admin",
		"title":       "排查生产 502",
		"providerId":  "sysadmin",
		"sandboxId":   "sb-seed",
		"assistantId": "as-seed",
		"status":      "idle",
		"createdAt":   now,
		"updatedAt":   now,
	}
	m := func(id, role, content string, meta map[string]any) map[string]any {
		out := map[string]any{
			"id": id, "sessionId": "sess-seed", "role": role,
			"content": content, "createdAt": now,
		}
		if meta != nil {
			out["meta"] = meta
		}
		return out
	}
	messages := []map[string]any{
		m("m-user-1", "user", "帮我看看为什么发布到生产环境之后接口偶尔会 502，超时时间怎么设置才合理？这个超长的英文单词也要能折行：\n\n"+long, nil),
		m("m-thought-1", "assistant", "先确认超时配置在哪里生效，再看看连接池和健康检查的指标。",
			map[string]any{"type": "reasoning", "status": "completed"}),
		m("m-tool-1", "assistant", "",
			map[string]any{
				"type": "function_call", "toolId": "tool-read-1", "title": "Read",
				"status": "completed",
				"input":  map[string]any{"path": "/workspace/nginx.conf"},
				"output": "proxy_read_timeout 30s;\nproxy_connect_timeout 5s;",
			}),
		m("m-assist-1", "assistant", "原因通常有三个：一是网关默认读超时 30 秒，慢查询直接掐断导致客户端看到 502；二是后端连接池被打满，新请求排队超过阈值直接拒绝；三是健康检查失败后负载均衡器仍在转发。\n\n建议按下面的顺序排查：\n\n1. 先看网关日志里 `upstream_timed_out` 的次数，排除超时问题。\n2. 再检查连接池大小与活跃连接数，如果持续打满就该扩容。\n3. 最后确认健康检查 path 是否快速返回。\n\n长英文词条折行验证："+long, nil),
		m("m-user-2", "user", "把超时改成 60 秒。", nil),
		m("m-clear-1", "event", "上下文已清空", map[string]any{"type": "clear"}),
	}
	// Slash command catalog: one builtin skill and the two action commands.
	commands := []map[string]any{
		{"name": "clear", "kind": "action", "source": "action", "description": "清空本会话上下文：聊天记录保留，模型从零开始"},
		{"name": "help", "kind": "action", "source": "action", "description": "显示可用命令"},
		{"name": "review", "kind": "skill", "source": "builtin", "description": "审查最近改动，给出 bug / 安全 / 可读性结论"},
		{"name": "commit", "kind": "skill", "source": "builtin", "description": "审阅工作区改动并生成规范的提交信息", "args": true},
	}
	return &seedSession{session: sess, messages: messages, commands: commands}
}
