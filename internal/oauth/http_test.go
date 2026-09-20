package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

type httpFixture struct {
	*flowFixture
	handler  *Handler
	sessions *storage.MemorySessionStore
	mux      *http.ServeMux
}

func newHTTPFixture(t *testing.T, remote *fakeRemote) *httpFixture {
	t.Helper()
	f := newFlowFixture(t, remote)
	sessions := storage.NewMemorySessionStore()
	h := &Handler{Svc: f.svc, Sessions: sessions}
	mux := http.NewServeMux()
	h.Mount(mux)

	f.addUser(t, storage.User{Username: "alice", Email: "alice@example.test", Role: storage.RoleUser})
	f.addUser(t, storage.User{Username: "root", Role: storage.RoleAdmin})
	return &httpFixture{flowFixture: f, handler: h, sessions: sessions, mux: mux}
}

func (h *httpFixture) user(t *testing.T, name string) *storage.User {
	t.Helper()
	u, err := h.users.GetByUsername(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (h *httpFixture) do(t *testing.T, method, path string, as *storage.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if as != nil {
		req = req.WithContext(auth.WithUser(req.Context(), as))
	}
	rr := httptest.NewRecorder()
	h.mux.ServeHTTP(rr, req)
	return rr
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
}

func TestHTTPListEnabledProviders(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	disabled := f.prov
	disabled.ID = "gitea-disabled"
	disabled.Enabled = false
	f.store.providers[disabled.ID] = disabled

	rr := f.do(t, http.MethodGet, "/v1/auth/oauth/providers", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body struct {
		Providers []providerView `json:"providers"`
	}
	decode(t, rr, &body)
	if len(body.Providers) != 1 || body.Providers[0].ID != f.prov.ID {
		t.Fatalf("providers = %+v", body.Providers)
	}
	if body.Providers[0].Label != "Gitea" {
		t.Errorf("label = %q", body.Providers[0].Label)
	}
}

func TestHTTPStartRedirectsToAuthorizeURL(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	rr := f.do(t, http.MethodGet, "/v1/auth/oauth/"+f.prov.ID+"/start?redirect=/settings/accounts", nil)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc.String(), f.prov.AuthURL) {
		t.Errorf("location = %q, want it to start with %q", loc, f.prov.AuthURL)
	}
	q := loc.Query()
	if q.Get("state") == "" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("authorize query = %v", q)
	}
	st, ok := f.store.states[q.Get("state")]
	if !ok {
		t.Fatal("state was not persisted")
	}
	if st.RedirectTo != "/settings/accounts" {
		t.Errorf("redirect_to = %q", st.RedirectTo)
	}
}

func TestHTTPStartUnknownProviderGoesToLogin(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	rr := f.do(t, http.MethodGet, "/v1/auth/oauth/nope/start", nil)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "/login?oauth_error=provider" {
		t.Errorf("location = %q", got)
	}
}

func TestHTTPCallbackIssuesSession(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":42,"login":"octo"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	start := f.do(t, http.MethodGet, "/v1/auth/oauth/"+f.prov.ID+"/start", nil)
	state := stateFromURL(t, start.Header().Get("Location"))

	rr := f.do(t, http.MethodGet, "/v1/auth/oauth/"+f.prov.ID+"/callback?code=c&state="+state, nil)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	if got := rr.Header().Get("Location"); got != "/" {
		t.Errorf("location = %q", got)
	}
	cookie := rr.Result().Cookies()
	if len(cookie) == 0 || cookie[0].Name != auth.SessionCookieName || cookie[0].Value == "" {
		t.Fatalf("cookies = %+v", cookie)
	}
	if _, err := f.sessions.GetByTokenHash(context.Background(), auth.HashSessionToken(cookie[0].Value)); err != nil {
		t.Errorf("session not stored: %v", err)
	}
}

func TestHTTPCallbackErrorsRedirect(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	rr := f.do(t, http.MethodGet, "/v1/auth/oauth/"+f.prov.ID+"/callback?code=c&state=bogus", nil)
	if got := rr.Header().Get("Location"); got != "/login?oauth_error=state" {
		t.Errorf("anonymous location = %q", got)
	}
	rr = f.do(t, http.MethodGet, "/v1/auth/oauth/"+f.prov.ID+"/callback?code=c&state=bogus", f.user(t, "alice"))
	if got := rr.Header().Get("Location"); got != "/settings/accounts?oauth_error=state" {
		t.Errorf("signed-in location = %q", got)
	}
}

func TestHTTPLinkFlow(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	if rr := f.do(t, http.MethodPost, "/v1/me/identities/link/"+f.prov.ID, nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", rr.Code)
	}

	rr := f.do(t, http.MethodPost, "/v1/me/identities/link/"+f.prov.ID, f.user(t, "alice"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body)
	}
	var body struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	decode(t, rr, &body)
	st, ok := f.store.states[stateFromURL(t, body.AuthorizeURL)]
	if !ok {
		t.Fatal("link flow did not persist a state")
	}
	if st.LinkUser != "alice" {
		t.Errorf("link_user = %q", st.LinkUser)
	}
	if st.RedirectTo != "/settings/accounts" {
		t.Errorf("redirect_to = %q", st.RedirectTo)
	}
}

func TestHTTPListIdentitiesNeverLeaksTokens(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1", Login: "octo",
		AccessToken: "super-secret-token", RefreshToken: "super-secret-refresh",
	}); err != nil {
		t.Fatal(err)
	}

	rr := f.do(t, http.MethodGet, "/v1/me/identities", f.user(t, "alice"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "super-secret") {
		t.Fatalf("response leaks token material: %s", rr.Body.String())
	}
	var body struct {
		Identities []identityView `json:"identities"`
	}
	decode(t, rr, &body)
	if len(body.Identities) != 1 {
		t.Fatalf("identities = %+v", body.Identities)
	}
	got := body.Identities[0]
	if got.Login != "octo" || got.ProviderLabel != "Gitea" || got.ProviderHost != f.prov.Host {
		t.Errorf("identity = %+v", got)
	}
	if !got.HasRefreshToken {
		t.Error("hasRefreshToken should be true")
	}
}

func TestHTTPUnlink(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	created, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1", Login: "octo", AccessToken: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rr := f.do(t, http.MethodDelete, "/v1/me/identities/missing", f.user(t, "alice")); rr.Code != http.StatusNotFound {
		t.Errorf("missing status = %d", rr.Code)
	}
	if rr := f.do(t, http.MethodDelete, "/v1/me/identities/"+created.ID, f.user(t, "alice")); rr.Code != http.StatusNoContent {
		t.Errorf("delete status = %d", rr.Code)
	}
	if _, err := f.store.GetIdentityBySubject(context.Background(), f.prov.ID, "1"); err == nil {
		t.Error("identity still present after unlink")
	}
}

func TestHTTPAdminProviderCRUD(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	if rr := f.do(t, http.MethodGet, "/v1/admin/oauth/providers", f.user(t, "alice")); rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d", rr.Code)
	}

	admin := f.user(t, "root")
	rr := f.do(t, http.MethodGet, "/v1/admin/oauth/providers", admin)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var list struct {
		Providers []adminProviderView `json:"providers"`
	}
	decode(t, rr, &list)
	if len(list.Providers) != 1 {
		t.Fatalf("providers = %+v", list.Providers)
	}
	if list.Providers[0].ClientSecret != settings.SecretMask {
		t.Errorf("client secret not masked: %q", list.Providers[0].ClientSecret)
	}
	wantCallback := "http://example.com/v1/auth/oauth/" + f.prov.ID + "/callback"
	if list.Providers[0].CallbackURL != wantCallback {
		t.Errorf("callback url = %q, want %q", list.Providers[0].CallbackURL, wantCallback)
	}

	// A masked secret means "keep the stored one".
	body := `{"id":"` + f.prov.ID + `","kind":"gitea","scheme":"http","host":"` + f.prov.Host +
		`","label":"Teams Gitea","clientId":"cid","clientSecret":"` + settings.SecretMask + `","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/oauth/providers", strings.NewReader(body))
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	rr = httptest.NewRecorder()
	f.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("save status = %d, body %s", rr.Code, rr.Body)
	}
	saved, err := f.store.GetProvider(context.Background(), f.prov.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ClientSecret != "sec" {
		t.Errorf("client secret = %q, want the stored one kept", saved.ClientSecret)
	}
	if saved.Label != "Teams Gitea" {
		t.Errorf("label = %q", saved.Label)
	}
	if saved.Scopes != DefaultScopes {
		t.Errorf("scopes = %q, want the default filled in", saved.Scopes)
	}

	bad := httptest.NewRequest(http.MethodPut, "/v1/admin/oauth/providers", strings.NewReader(`{"id":"x","kind":"gitlab","host":"gitlab.com"}`))
	bad = bad.WithContext(auth.WithUser(bad.Context(), admin))
	rr = httptest.NewRecorder()
	f.mux.ServeHTTP(rr, bad)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid kind status = %d", rr.Code)
	}

	if rr := f.do(t, http.MethodDelete, "/v1/admin/oauth/providers/"+f.prov.ID, admin); rr.Code != http.StatusNoContent {
		t.Errorf("delete status = %d", rr.Code)
	}
	if rr := f.do(t, http.MethodDelete, "/v1/admin/oauth/providers/"+f.prov.ID, admin); rr.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d", rr.Code)
	}
}
