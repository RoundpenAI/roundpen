package agentapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type stubEnvStore struct{ browserID string }

func (s stubEnvStore) Get(_ context.Context, userID, slot string) (*userenv.Mapping, error) {
	if slot != userenv.SlotBrowser || s.browserID == "" {
		return nil, nil
	}
	return &userenv.Mapping{UserID: userID, Slot: slot, SandboxID: s.browserID}, nil
}

func (s stubEnvStore) List(context.Context, string) ([]userenv.Mapping, error) { return nil, nil }

func (s stubEnvStore) Upsert(context.Context, string, string, string, string) error { return nil }

func (s stubEnvStore) Delete(context.Context, string, string) error { return nil }

func newBrowserHandler(provider, endpoint, staleID string) *Handler {
	return &Handler{Envs: &userenv.Service{
		Store: stubEnvStore{browserID: staleID},
		Cfg: &config.Config{CDP: config.CDPConfig{
			Provider: provider,
			Endpoint: endpoint,
		}},
	}}
}

func requestWithUser(username string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(auth.WithUser(r.Context(), &storage.User{Username: username}))
}

func TestHubKeyFollowsProvider(t *testing.T) {
	sess := &agentsession.Session{ID: "sess-1", UserID: "alice"}
	req := requestWithUser("alice")

	// Non-docker providers must ignore a stale managed-container mapping: the
	// key has to match what EnsureBrowser hands out.
	h := newBrowserHandler(config.CDPProviderRemote, "ws://10.10.1.3:3000/chrome", "sandbox-stale")
	if got, want := h.hubKey(req, sess), userenv.BrowserKey("alice"); got != want {
		t.Fatalf("remote hubKey=%q want %q", got, want)
	}
	if got, want := h.ensureHubKey(req, sess), userenv.BrowserKey("alice"); got != want {
		t.Fatalf("remote ensureHubKey=%q want %q", got, want)
	}

	// Docker keeps the mapped container id while one exists.
	h = newBrowserHandler(config.CDPProviderDocker, "", "sandbox-live")
	if got := h.hubKey(req, sess); got != "sandbox-live" {
		t.Fatalf("docker hubKey=%q want sandbox-live", got)
	}
}

func TestEnsureHubKeyNonDockerFallback(t *testing.T) {
	// Remote without an endpoint fails EnsureBrowser; the fallback must be the
	// stable user key, not the stale sandbox mapping.
	h := newBrowserHandler(config.CDPProviderRemote, "", "sandbox-stale")
	sess := &agentsession.Session{ID: "sess-1", UserID: "alice"}
	if got, want := h.ensureHubKey(requestWithUser("alice"), sess), userenv.BrowserKey("alice"); got != want {
		t.Fatalf("ensureHubKey=%q want %q", got, want)
	}
}

func TestEnsureHubKeyDockerFallsBackToMapping(t *testing.T) {
	// Docker EnsureBrowser fails here (no sandbox manager); the mapped id is
	// still the best key to poll.
	h := newBrowserHandler(config.CDPProviderDocker, "", "sandbox-live")
	sess := &agentsession.Session{ID: "sess-1", UserID: "alice"}
	if got := h.ensureHubKey(requestWithUser("alice"), sess); got != "sandbox-live" {
		t.Fatalf("ensureHubKey=%q want sandbox-live", got)
	}
}

func TestHubKeyWithoutEnvsFallsBackToSession(t *testing.T) {
	h := &Handler{}
	sess := &agentsession.Session{ID: "sess-1", UserID: "alice"}
	if got := h.hubKey(httptest.NewRequest(http.MethodGet, "/", nil), sess); got != "sysagent-sess-1" {
		t.Fatalf("hubKey=%q want sysagent-sess-1", got)
	}
}
