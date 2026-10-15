package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseResourceMetadata(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{`Bearer resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource"`, "https://mcp.example.test/.well-known/oauth-protected-resource"},
		{`Bearer resource_metadata=https://a.test/prm, error="invalid_token"`, "https://a.test/prm"},
		{`Bearer realm="x"`, ""},
		{``, ""},
	}
	for _, tc := range cases {
		if got := parseResourceMetadata(tc.header); got != tc.want {
			t.Errorf("parseResourceMetadata(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestWellKnownURL(t *testing.T) {
	got, err := wellKnownURL("https://gitlab.com/api/v4/mcp", "/.well-known/oauth-protected-resource")
	if err != nil || got != "https://gitlab.com/.well-known/oauth-protected-resource/api/v4/mcp" {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = wellKnownURL("https://auth.example.test", "")
	if err != nil || got != "https://auth.example.test/.well-known/oauth-authorization-server" {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = wellKnownURL("https://auth.example.test/tenant/", "")
	if err != nil || got != "https://auth.example.test/.well-known/oauth-authorization-server/tenant" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := wellKnownURL("not a url", ""); err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

func TestPickTokenAuthMethod(t *testing.T) {
	if got := pickTokenAuthMethod([]string{"client_secret_basic", "none"}); got != "none" {
		t.Errorf("none preferred: %q", got)
	}
	if got := pickTokenAuthMethod([]string{"client_secret_post"}); got != "client_secret_post" {
		t.Errorf("post fallback: %q", got)
	}
	if got := pickTokenAuthMethod(nil); got != "none" {
		t.Errorf("default: %q", got)
	}
}

func TestSafeRedirect(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "/settings/mcp"},
		{"/settings/mcp?x=1", "/settings/mcp?x=1"},
		{"//evil.test", "/settings/mcp"},
		{"https://evil.test", "/settings/mcp"},
		{"relative", "/settings/mcp"},
	}
	for _, tc := range cases {
		if got := safeRedirect(tc.in); got != tc.want {
			t.Errorf("safeRedirect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDiscoverViaWWWAuthenticate(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              srv.URL + "/mcp",
			"authorization_servers": []string{srv.URL},
			"scopes_supported":      []string{"mcp.read", "mcp.write"},
		})
	})

	prm, err := Discover(context.Background(), srv.Client(), srv.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if prm.Resource != srv.URL+"/mcp" || len(prm.AuthorizationServers) != 1 {
		t.Fatalf("prm = %+v", prm)
	}
	if len(prm.ScopesSupported) != 2 {
		t.Fatalf("scopes = %v", prm.ScopesSupported)
	}
}

func TestDiscoverViaWellKnownFallback(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // no WWW-Authenticate
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_servers": []string{srv.URL},
		})
	})

	prm, err := Discover(context.Background(), srv.Client(), srv.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	// Resource defaults to the MCP endpoint when the document omits it.
	if prm.Resource != srv.URL+"/mcp" {
		t.Fatalf("prm = %+v", prm)
	}
}

func TestDiscoverMissingMetadata(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := Discover(context.Background(), srv.Client(), srv.URL+"/mcp"); err == nil {
		t.Fatal("expected discovery failure")
	}
}

func TestFetchASMetadata(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                           srv.URL,
			"authorization_endpoint":           srv.URL + "/authorize",
			"token_endpoint":                   srv.URL + "/token",
			"registration_endpoint":            srv.URL + "/register",
			"token_endpoint_auth_methods_supported": []string{"none"},
		})
	})
	meta, err := FetchASMetadata(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if meta.AuthorizationEndpoint != srv.URL+"/authorize" || meta.RegistrationEndpoint != srv.URL+"/register" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestFetchASMetadataMissingEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"issuer":"`+`x`+`"}`)
	}))
	defer srv.Close()
	if _, err := FetchASMetadata(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected error for metadata without endpoints")
	}
}

func TestRegisterClient(t *testing.T) {
	var gotAuthMethod string
	var gotRedirect string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotAuthMethod, _ = payload["token_endpoint_auth_method"].(string)
		if uris, ok := payload["redirect_uris"].([]any); ok && len(uris) == 1 {
			gotRedirect, _ = uris[0].(string)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-1"})
	}))
	defer srv.Close()

	meta := &ASMetadata{
		RegistrationEndpoint:     srv.URL + "/register",
		TokenEndpointAuthMethods: []string{"none"},
	}
	clientID, clientSecret, err := RegisterClient(context.Background(), srv.Client(), meta, "https://app.test/v1/connectors/mcp/callback")
	if err != nil {
		t.Fatal(err)
	}
	if clientID != "client-1" || clientSecret != "" {
		t.Fatalf("client = %q/%q", clientID, clientSecret)
	}
	if gotAuthMethod != "none" {
		t.Errorf("auth method = %q", gotAuthMethod)
	}
	if gotRedirect != "https://app.test/v1/connectors/mcp/callback" {
		t.Errorf("redirect = %q", gotRedirect)
	}
}

func TestRegisterClientUnsupported(t *testing.T) {
	_, _, err := RegisterClient(context.Background(), http.DefaultClient, &ASMetadata{TokenEndpoint: "https://as.test/token"}, "https://app.test/cb")
	if !errors.Is(err, ErrDCRUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestExchangeCodeAndRefresh(t *testing.T) {
	var lastForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		lastForm = r.PostForm
		if r.PostForm.Get("grant_type") == "authorization_code" && r.PostForm.Get("code") == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "code expired"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at",
			"refresh_token": "rt",
			"expires_in":    3600,
			"scope":         "mcp.read",
		})
	}))
	defer srv.Close()

	tok, err := ExchangeCode(context.Background(), srv.Client(), srv.URL+"/token", "cid", "sec", "code-1", "verifier-1", "https://app.test/cb", "https://mcp.test/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rt" || tok.ExpiresAt == nil {
		t.Fatalf("tokens = %+v", tok)
	}
	if lastForm.Get("code_verifier") != "verifier-1" || lastForm.Get("resource") != "https://mcp.test/mcp" ||
		lastForm.Get("client_secret") != "sec" || lastForm.Get("redirect_uri") != "https://app.test/cb" {
		t.Fatalf("form = %v", lastForm)
	}

	if _, err := ExchangeCode(context.Background(), srv.Client(), srv.URL+"/token", "cid", "", "bad", "v", "https://app.test/cb", ""); !IsInvalidGrant(err) {
		t.Fatalf("expected invalid_grant, got %v", err)
	}

	tok, err = RefreshToken(context.Background(), srv.Client(), srv.URL+"/token", "cid", "", "rt", "https://mcp.test/mcp")
	if err != nil || tok.AccessToken != "at" {
		t.Fatalf("refresh: %+v, %v", tok, err)
	}
	if lastForm.Get("grant_type") != "refresh_token" || lastForm.Get("refresh_token") != "rt" {
		t.Fatalf("refresh form = %v", lastForm)
	}
}

func TestRefreshTokenNoEndpoint(t *testing.T) {
	if _, err := RefreshToken(context.Background(), http.DefaultClient, "", "c", "", "r", ""); err == nil {
		t.Fatal("expected error without token endpoint")
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	meta := &ASMetadata{AuthorizationEndpoint: "https://as.test/authorize"}
	raw := BuildAuthorizeURL(meta, "cid", "st", "challenge", "https://app.test/cb", "https://mcp.test/mcp", "a b")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("state") != "st" || q.Get("code_challenge") != "challenge" ||
		q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" ||
		q.Get("resource") != "https://mcp.test/mcp" || q.Get("scope") != "a b" {
		t.Fatalf("query = %v", q)
	}
}

func TestRevokeToken(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	RevokeToken(context.Background(), srv.Client(), srv.URL+"/revoke", "cid", "sec", "tok")
	if got.Get("token") != "tok" || got.Get("client_id") != "cid" || got.Get("client_secret") != "sec" {
		t.Fatalf("form = %v", got)
	}
}

func TestIssuerFromTokenEndpoint(t *testing.T) {
	if got := issuerFromTokenEndpoint("https://as.example.test/oauth/token"); got != "https://as.example.test" {
		t.Fatalf("got %q", got)
	}
	if got := issuerFromTokenEndpoint("garbage"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCallbackURL(t *testing.T) {
	got := CallbackURL("http", "localhost:5173")
	want := "http://localhost:5173/v1/connectors/mcp/callback"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(CallbackURL("", ""), "https://") {
		t.Fatal("default scheme must be https")
	}
}
