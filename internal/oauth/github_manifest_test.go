package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildGitHubAppManifest(t *testing.T) {
	m := BuildGitHubAppManifest("https", "console.test")
	if m.Name != "Roundpen" {
		t.Errorf("name = %q", m.Name)
	}
	if m.URL != "https://console.test" {
		t.Errorf("url = %q", m.URL)
	}
	if m.RedirectURL != "https://console.test"+githubManifestCallbackPath {
		t.Errorf("redirect = %q", m.RedirectURL)
	}
	if len(m.CallbackURLs) != 1 || m.CallbackURLs[0] != "https://console.test/v1/auth/oauth/github/callback" {
		t.Errorf("callback_urls = %v", m.CallbackURLs)
	}
	if m.RequestOAuthOnInstall {
		t.Error("request_oauth_on_install would skip PKCE; keep it false")
	}
	if !m.Public {
		t.Error("app should be public so other instance users can install it")
	}
	for _, key := range []string{"contents", "metadata", "pull_requests", "issues", "email"} {
		if m.DefaultPermissions[key] == "" {
			t.Errorf("missing permission %q", key)
		}
	}
}

func TestStartGitHubManifestPersistsState(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t})
	out, err := f.svc.StartGitHubManifest(context.Background(), "root", "https", "console.test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.ActionURL, "https://github.com/settings/apps/new?state=") {
		t.Errorf("actionUrl = %q", out.ActionURL)
	}
	if out.State == "" || !strings.Contains(out.ActionURL, out.State) {
		t.Errorf("state missing from action url: %+v", out)
	}
	if out.CallbackURL != "https://console.test/v1/auth/oauth/github/callback" {
		t.Errorf("callbackUrl = %q", out.CallbackURL)
	}
	var parsed GitHubAppManifest
	if err := json.Unmarshal([]byte(out.Manifest), &parsed); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if parsed.RedirectURL != "https://console.test"+githubManifestCallbackPath {
		t.Errorf("manifest redirect = %q", parsed.RedirectURL)
	}
	st, ok := f.store.states[out.State]
	if !ok {
		t.Fatal("state was not persisted")
	}
	if st.ProviderID != githubManifestProviderID || st.LinkUser != "root" {
		t.Errorf("state = %+v", st)
	}
}

func TestCompleteGitHubManifestSavesProvider(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t})
	convert := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/app-manifests/tmp-code/conversions") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		_, _ = io.WriteString(w, `{"client_id":"Iv1.new","client_secret":"s3cret","name":"Roundpen"}`)
	}))
	t.Cleanup(convert.Close)
	f.svc.HTTP = convert.Client()
	f.svc.GitHubConvertURL = func(code string) string {
		return convert.URL + "/app-manifests/" + code + "/conversions"
	}

	start, err := f.svc.StartGitHubManifest(context.Background(), "root", "https", "console.test")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.svc.CompleteGitHubManifest(context.Background(), "tmp-code", start.State, "root")
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != "github" || saved.Host != "github.com" || !saved.Enabled {
		t.Errorf("saved = %+v", saved)
	}
	if saved.ClientID != "Iv1.new" || saved.ClientSecret != "s3cret" {
		t.Errorf("credentials = %q / %q", saved.ClientID, saved.ClientSecret)
	}

	// A second handshake updates the same github.com row instead of inserting
	// a colliding (kind, host) pair.
	start2, err := f.svc.StartGitHubManifest(context.Background(), "root", "https", "console.test")
	if err != nil {
		t.Fatal(err)
	}
	saved2, err := f.svc.CompleteGitHubManifest(context.Background(), "tmp-code", start2.State, "root")
	if err != nil {
		t.Fatal(err)
	}
	if saved2.ID != "github" {
		t.Errorf("second save id = %q", saved2.ID)
	}
}

func TestCompleteGitHubManifestRejectsBadState(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t})
	if _, err := f.svc.CompleteGitHubManifest(context.Background(), "code", "missing", "root"); err == nil {
		t.Fatal("expected error")
	}
	start, err := f.svc.StartGitHubManifest(context.Background(), "root", "https", "console.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CompleteGitHubManifest(context.Background(), "code", start.State, "alice"); err == nil {
		t.Fatal("expected session mismatch")
	}
}

func TestHTTPGitHubManifestStartAndCallback(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})
	if rr := f.do(t, http.MethodGet, "/v1/admin/oauth/github/manifest", f.user(t, "alice")); rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d", rr.Code)
	}

	convert := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"client_id":"Iv1.http","client_secret":"from-manifest"}`)
	}))
	t.Cleanup(convert.Close)
	f.svc.HTTP = convert.Client()
	f.svc.GitHubConvertURL = func(code string) string {
		return convert.URL + "/app-manifests/" + code + "/conversions"
	}

	rr := f.do(t, http.MethodGet, "/v1/admin/oauth/github/manifest", f.user(t, "root"))
	if rr.Code != http.StatusOK {
		t.Fatalf("start status = %d, body %s", rr.Code, rr.Body)
	}
	var start GitHubManifestStart
	decode(t, rr, &start)
	if start.State == "" || start.Manifest == "" {
		t.Fatalf("start = %+v", start)
	}

	cb := f.do(t, http.MethodGet, githubManifestCallbackPath+"?code=tmp-code&state="+start.State, f.user(t, "root"))
	if cb.Code != http.StatusFound {
		t.Fatalf("callback status = %d, body %s", cb.Code, cb.Body)
	}
	if loc := cb.Header().Get("Location"); loc != githubSettingsPath+"?github_app=1" {
		t.Errorf("location = %q", loc)
	}
	saved, err := f.store.GetProvider(context.Background(), "github")
	if err != nil {
		t.Fatal(err)
	}
	if saved.ClientID != "Iv1.http" || saved.ClientSecret != "from-manifest" {
		t.Errorf("saved credentials = %+v", saved)
	}
}

func TestHTTPGitHubManifestCallbackErrors(t *testing.T) {
	f := newHTTPFixture(t, &fakeRemote{t: t})

	anon := f.do(t, http.MethodGet, githubManifestCallbackPath+"?code=c&state=x", nil)
	if got := anon.Header().Get("Location"); got != githubSettingsPath+"?github_app_error=session" {
		t.Errorf("anonymous location = %q", got)
	}

	user := f.do(t, http.MethodGet, githubManifestCallbackPath+"?code=c&state=x", f.user(t, "alice"))
	if got := user.Header().Get("Location"); got != githubSettingsPath+"?github_app_error=session" {
		t.Errorf("non-admin location = %q", got)
	}

	denied := f.do(t, http.MethodGet, githubManifestCallbackPath+"?error=access_denied", f.user(t, "root"))
	if got := denied.Header().Get("Location"); got != githubSettingsPath+"?github_app_error=denied" {
		t.Errorf("denied location = %q", got)
	}

	bad := f.do(t, http.MethodGet, githubManifestCallbackPath+"?code=c&state=bogus", f.user(t, "root"))
	if got := bad.Header().Get("Location"); got != githubSettingsPath+"?github_app_error=state" {
		t.Errorf("bad state location = %q", got)
	}
}
