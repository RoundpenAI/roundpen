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
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeEnvs struct {
	force  bool
	calls  int
	user   string
	result *userenv.UpgradeResult
	err    error
}

func (f *fakeEnvs) List(context.Context, string) ([]userenv.EnvView, error) { return nil, nil }
func (f *fakeEnvs) EnsureBrowser(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: "browser-1", Status: sandbox.StatusRunning}, nil
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
