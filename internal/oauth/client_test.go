package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRemote serves the token endpoint plus /user and /user/emails, recording
// what the client sent so tests can assert on the wire format.
type fakeRemote struct {
	t            *testing.T
	tokenOK      func(form map[string]string) (int, string)
	user         string
	emails       string
	emailsStatus int // 0 = 200

	lastForm map[string]string
	lastAuth string
}

func (f *fakeRemote) start() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			f.t.Errorf("parse form: %v", err)
		}
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		f.lastForm = form
		tokenOK := f.tokenOK
		if tokenOK == nil {
			tokenOK = func(map[string]string) (int, string) {
				return http.StatusOK, `{"access_token":"at-1"}`
			}
		}
		status, body := tokenOK(form)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.user))
	})
	mux.HandleFunc("GET /api/v1/user/emails", func(w http.ResponseWriter, _ *http.Request) {
		if f.emailsStatus != 0 {
			w.WriteHeader(f.emailsStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.emails))
	})
	return httptest.NewServer(mux)
}

func newFakeProvider(t *testing.T, srv *httptest.Server) Provider {
	t.Helper()
	base := strings.TrimPrefix(srv.URL, "http://")
	p, err := Normalize(Provider{Kind: KindGitea, Scheme: "http", Host: base, ClientID: "cid", ClientSecret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExchangeSendsPKCEAndParsesToken(t *testing.T) {
	f := &fakeRemote{
		t: t,
		tokenOK: func(form map[string]string) (int, string) {
			if form["grant_type"] != "authorization_code" || form["code"] != "the-code" {
				t.Errorf("form = %v", form)
			}
			return http.StatusOK, `{"access_token":"at-1","token_type":"bearer","scope":"read:user,user:email","expires_in":3600}`
		},
	}
	srv := f.start()
	defer srv.Close()

	c := &Client{Provider: newFakeProvider(t, srv)}
	tok, err := c.Exchange(context.Background(), "the-code", "verifier-1", "https://console.test/cb")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at-1" {
		t.Errorf("access token = %q", tok.AccessToken)
	}
	if tok.Scopes != "read:user user:email" {
		t.Errorf("scopes = %q", tok.Scopes)
	}
	if tok.ExpiresAt == nil {
		t.Error("expires_in should produce an expiry")
	}
	for key, want := range map[string]string{
		"client_id":     "cid",
		"client_secret": "secret",
		"code_verifier": "verifier-1",
		"redirect_uri":  "https://console.test/cb",
	} {
		if f.lastForm[key] != want {
			t.Errorf("token request %s = %q, want %q", key, f.lastForm[key], want)
		}
	}
}

func TestExchangeWithoutExpiryAndRefreshRotation(t *testing.T) {
	f := &fakeRemote{
		t: t,
		tokenOK: func(form map[string]string) (int, string) {
			if form["grant_type"] == "refresh_token" {
				if form["refresh_token"] != "rt-1" {
					t.Errorf("refresh token = %q", form["refresh_token"])
				}
				return http.StatusOK, `{"access_token":"at-2","refresh_token":"rt-2"}`
			}
			return http.StatusOK, `{"access_token":"at-1"}`
		},
	}
	srv := f.start()
	defer srv.Close()

	c := &Client{Provider: newFakeProvider(t, srv)}
	tok, err := c.Exchange(context.Background(), "code", "", "https://console.test/cb")
	if err != nil {
		t.Fatal(err)
	}
	if tok.ExpiresAt != nil {
		t.Error("missing expires_in should mean no expiry (GitHub default)")
	}

	refreshed, err := c.Refresh(context.Background(), "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken != "at-2" || refreshed.RefreshToken != "rt-2" {
		t.Errorf("refreshed = %+v", refreshed)
	}
}

func TestExchangeSurfacesProviderError(t *testing.T) {
	f := &fakeRemote{
		t: t,
		tokenOK: func(map[string]string) (int, string) {
			return http.StatusOK, `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`
		},
	}
	srv := f.start()
	defer srv.Close()

	c := &Client{Provider: newFakeProvider(t, srv)}
	_, err := c.Exchange(context.Background(), "code", "", "https://console.test/cb")
	if err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Fatalf("err = %v", err)
	}
}

func TestProfilePicksVerifiedPrimaryEmail(t *testing.T) {
	f := &fakeRemote{
		t:      t,
		user:   `{"id":42,"login":"octo","full_name":"Octo Cat","email":"public@example.test"}`,
		emails: `[{"email":"other@example.test","primary":false,"verified":true},{"email":"octo@example.test","primary":true,"verified":true},{"email":"stale@example.test","primary":false,"verified":false}]`,
	}
	srv := f.start()
	defer srv.Close()

	c := &Client{Provider: newFakeProvider(t, srv)}
	p, err := c.Profile(context.Background(), "at-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != "42" || p.Login != "octo" || p.Name != "Octo Cat" {
		t.Errorf("profile = %+v", p)
	}
	if p.Email != "public@example.test" {
		t.Errorf("public email = %q", p.Email)
	}
	if p.VerifiedEmail != "octo@example.test" {
		t.Errorf("verified email = %q", p.VerifiedEmail)
	}
	if f.lastAuth != "Bearer at-1" {
		t.Errorf("authorization = %q", f.lastAuth)
	}
}

func TestProfileFallsBackWhenEmailsForbidden(t *testing.T) {
	f := &fakeRemote{
		t:            t,
		user:         `{"id":7,"login":"octo"}`,
		emailsStatus: http.StatusForbidden,
	}
	srv := f.start()
	defer srv.Close()

	c := &Client{Provider: newFakeProvider(t, srv)}
	p, err := c.Profile(context.Background(), "at-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.VerifiedEmail != "" {
		t.Errorf("verified email = %q, want empty", p.VerifiedEmail)
	}
	if p.Name != "" {
		t.Errorf("name = %q", p.Name)
	}
}

func TestProfileRequiresIDAndLogin(t *testing.T) {
	f := &fakeRemote{t: t, user: `{"name":"no id"}`, emails: `[]`}
	srv := f.start()
	defer srv.Close()

	c := &Client{Provider: newFakeProvider(t, srv)}
	if _, err := c.Profile(context.Background(), "at-1"); err == nil {
		t.Fatal("expected error for a profile without id/login")
	}
}

func TestGitHubProfileUsesGitHubAcceptHeader(t *testing.T) {
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/emails") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{"id":1,"login":"octo"}`))
	}))
	defer srv.Close()

	p, err := Normalize(Provider{Kind: KindGitHub, Scheme: "http", Host: strings.TrimPrefix(srv.URL, "http://"),
		APIURL: srv.URL, TokenURL: srv.URL, AuthURL: srv.URL, ClientID: "cid", ClientSecret: "sec", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Provider: p}
	if _, err := c.Profile(context.Background(), "at-1"); err != nil {
		t.Fatal(err)
	}
	if accept != "application/vnd.github+json" {
		t.Errorf("accept = %q", accept)
	}
}
