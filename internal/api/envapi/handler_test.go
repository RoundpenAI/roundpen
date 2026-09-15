package envapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeEnvs struct {
	force  bool
	calls  int
	user   string
	result *userenv.UpgradeResult
	err    error
	target *userenv.BrowserTarget
}

func (f *fakeEnvs) List(context.Context, string) ([]userenv.EnvView, error) { return nil, nil }
func (f *fakeEnvs) EnsureBrowser(context.Context, string) (*userenv.BrowserTarget, error) {
	f.calls++
	return f.target, f.err
}
func (f *fakeEnvs) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: "agent-1", Status: sandbox.StatusRunning}, nil
}
func (f *fakeEnvs) UpgradeAgent(_ context.Context, userID string, force bool) (*userenv.UpgradeResult, error) {
	f.calls++
	f.force = force
	f.user = userID
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &userenv.UpgradeResult{Status: "up_to_date", Image: "img:1", Digest: "sha256:x"}, nil
}

func (f *fakeEnvs) RecreateAgent(_ context.Context, userID string) (*userenv.UpgradeResult, error) {
	f.calls++
	f.user = userID
	if f.err != nil {
		return nil, f.err
	}
	return &userenv.UpgradeResult{Status: "recreated", Image: "img:1"}, nil
}

func (f *fakeEnvs) RecreateBrowser(_ context.Context, userID string) (*userenv.UpgradeResult, error) {
	f.calls++
	f.user = userID
	if f.err != nil {
		return nil, f.err
	}
	return &userenv.UpgradeResult{Status: "recreated", Image: "img:browser"}, nil
}

func modelSourceRequest(t *testing.T, envs *fakeEnvs, users *storage.MemoryUserStore, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&Handler{Envs: envs, Users: users}).Mount(mux)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/v1/me/model-source", reader)
	req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: "alice", Role: storage.RoleUser}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestModelSourceGetAndPut(t *testing.T) {
	envs := &fakeEnvs{}
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(context.Background(), storage.User{Username: "alice", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}

	rec := modelSourceRequest(t, envs, users, http.MethodGet, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), storage.ModelSourceGateway) {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	rec = modelSourceRequest(t, envs, users, http.MethodPut, `{"modelSource":"own"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ModelSource string `json:"modelSource"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ModelSource != storage.ModelSourceOwn || resp.Status != "recreated" {
		t.Fatalf("put resp: %+v", resp)
	}
	if envs.calls != 1 {
		t.Fatalf("expected one rebuild, got %d", envs.calls)
	}
	u, err := users.GetByUsername(context.Background(), "alice")
	if err != nil || u.ModelSource != storage.ModelSourceOwn {
		t.Fatalf("stored: %+v err=%v", u, err)
	}

	rec = modelSourceRequest(t, envs, users, http.MethodPut, `{"modelSource":"bogus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid value: %d %s", rec.Code, rec.Body.String())
	}
}

func proxyRequest(t *testing.T, envs *fakeEnvs, users *storage.MemoryUserStore, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&Handler{
		Envs:  envs,
		Users: users,
		Proxies: func() []settings.ProxyProfile {
			return []settings.ProxyProfile{
				{ID: "us", Name: "US egress", URL: "socks5://10.0.0.9:1080"},
				{ID: "jp", Name: "JP egress", URL: "http://user:pass@10.0.0.8:8080"},
			}
		},
	}).Mount(mux)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: "alice", Role: storage.RoleUser}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestProxySelection(t *testing.T) {
	envs := &fakeEnvs{}
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(context.Background(), storage.User{Username: "alice", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}

	rec := proxyRequest(t, envs, users, http.MethodGet, "/v1/me/proxies", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "US egress") {
		t.Fatalf("profiles missing: %s", body)
	}
	if strings.Contains(body, "10.0.0.9") || strings.Contains(body, "pass") {
		t.Fatalf("proxy URLs/creds must not leak to users: %s", body)
	}

	rec = proxyRequest(t, envs, users, http.MethodPut, "/v1/me/proxy", `{"slot":"browser","profileId":"us"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	if envs.calls != 1 {
		t.Fatalf("expected one browser rebuild, got %d", envs.calls)
	}
	u, err := users.GetByUsername(context.Background(), "alice")
	if err != nil || u.BrowserProxy != "us" || u.AgentProxy != "" {
		t.Fatalf("stored: %+v err=%v", u, err)
	}

	rec = proxyRequest(t, envs, users, http.MethodPut, "/v1/me/proxy", `{"slot":"agent","profileId":"nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown profile: %d %s", rec.Code, rec.Body.String())
	}
	rec = proxyRequest(t, envs, users, http.MethodPut, "/v1/me/proxy", `{"slot":"mobile","profileId":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad slot: %d %s", rec.Code, rec.Body.String())
	}

	// Clearing back to direct is allowed without a profile lookup.
	rec = proxyRequest(t, envs, users, http.MethodPut, "/v1/me/proxy", `{"slot":"browser","profileId":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
}

func upgradeRequest(t *testing.T, envs *fakeEnvs, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&Handler{Envs: envs}).Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/environments/agent/upgrade", strings.NewReader(body))
	req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: "alice", Role: storage.RoleUser}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestUpgradeAgentEndpoint_OK(t *testing.T) {
	envs := &fakeEnvs{result: &userenv.UpgradeResult{
		Status: "upgraded", Image: "img:2", Digest: "sha256:y",
		Environment: userenv.EnvView{Slot: userenv.SlotAgent, Status: "running", Image: "img:2"},
	}}
	rec := upgradeRequest(t, envs, `{"force":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Status      string `json:"status"`
		Image       string `json:"image"`
		Digest      string `json:"digest"`
		Environment struct {
			Slot   string `json:"slot"`
			Status string `json:"status"`
			Image  string `json:"image"`
		} `json:"environment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "upgraded" || out.Image != "img:2" || out.Digest != "sha256:y" {
		t.Fatalf("out=%+v", out)
	}
	if out.Environment.Slot != "agent" || out.Environment.Status != "running" || out.Environment.Image != "img:2" {
		t.Fatalf("environment=%+v", out.Environment)
	}
	if envs.force {
		t.Fatal("force should be false")
	}
	if envs.user != "alice" {
		t.Fatalf("user=%q, want alice", envs.user)
	}
	if envs.calls != 1 {
		t.Fatalf("calls=%d, want 1", envs.calls)
	}
}

func TestUpgradeAgentEndpoint_ForceFlag(t *testing.T) {
	envs := &fakeEnvs{}
	rec := upgradeRequest(t, envs, `{"force":true}`)
	if rec.Code != http.StatusOK || !envs.force {
		t.Fatalf("status=%d force=%v", rec.Code, envs.force)
	}
}

func TestUpgradeAgentEndpoint_MalformedBody(t *testing.T) {
	envs := &fakeEnvs{}
	rec := upgradeRequest(t, envs, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if envs.calls != 0 {
		t.Fatalf("service must not be called: calls=%d", envs.calls)
	}
}

func TestUpgradeAgentEndpoint_EmptyBody(t *testing.T) {
	envs := &fakeEnvs{}
	if rec := upgradeRequest(t, envs, ""); rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpgradeAgentEndpoint_ServiceError(t *testing.T) {
	envs := &fakeEnvs{err: errors.New("registry unreachable")}
	rec := upgradeRequest(t, envs, `{"force":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "registry unreachable") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
