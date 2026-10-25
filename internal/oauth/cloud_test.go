package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

type fakeInstall struct{}

func (fakeInstall) MintToken() (string, error) { return "rp1.test.1.sig", nil }
func (fakeInstall) PublicKeyHex() string       { return strings.Repeat("ab", 32) }

func TestCloudGitHubLinkInjectsRepoToken(t *testing.T) {
	var sawStart bool
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rp1.test.1.sig" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Roundpen-Install-Pub") != strings.Repeat("ab", 32) {
			t.Errorf("install pub missing")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/github/start":
			sawStart = true
			var body struct {
				Return string `json:"return"`
				State  string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Return != "https://console.test" || body.State == "" {
				t.Fatalf("start body %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorizeUrl": "https://github.com/login/oauth/authorize?state=" + body.State,
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tickets/"):
			if r.URL.Query().Get("state") == "" {
				t.Errorf("ticket redeem missing state")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"login": "octocat", "name": "Octo", "email": "octo@example.test",
				"accessToken": "gho_operate", "scope": "repo,read:user,user:email",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	store := newFakeStore()
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(context.Background(), storage.User{
		Username: "alice", Email: "alice@example.test", APIKey: "rp-alice", Role: storage.RoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Store:       store,
		Users:       users,
		GitHubRelay: relay.URL,
		Install:     fakeInstall{},
		HTTP:        relay.Client(),
	}

	if _, err := svc.AuthorizeURL(context.Background(), StartInput{
		ProviderID: CloudGitHubProviderID, ConsoleScheme: "https", ConsoleHost: "console.test",
	}); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("anonymous start err = %v, want unavailable", err)
	}

	raw, err := svc.AuthorizeURL(context.Background(), StartInput{
		ProviderID:    CloudGitHubProviderID,
		LinkUser:      "alice",
		RedirectTo:    "/settings/apps",
		ConsoleScheme: "https",
		ConsoleHost:   "console.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawStart || !strings.Contains(raw, "github.com/login/oauth/authorize") {
		t.Fatalf("authorize url %q sawStart=%v", raw, sawStart)
	}
	state := stateFromURL(t, raw)
	res, err := svc.HandleCloudCallback(context.Background(), CloudCallbackInput{
		Ticket: "abc", State: state, SessionUser: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.User.Username != "alice" || res.RedirectTo != "/settings/apps" {
		t.Fatalf("callback %+v", res)
	}

	httpx.SetUIBase("/app/roundpen")
	t.Cleanup(func() { httpx.SetUIBase("") })
	var returnWithPrefix string
	relayPrefixed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/github/start" {
			var body struct {
				Return string `json:"return"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			returnWithPrefix = body.Return
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorizeUrl": "https://github.com/login/oauth/authorize?state=x",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer relayPrefixed.Close()
	svc.GitHubRelay = relayPrefixed.URL
	svc.HTTP = relayPrefixed.Client()
	if _, err := svc.AuthorizeURL(context.Background(), StartInput{
		ProviderID:    CloudGitHubProviderID,
		LinkUser:      "alice",
		RedirectTo:    "/app/roundpen/settings/apps",
		ConsoleScheme: "https",
		ConsoleHost:   "nas.example",
	}); err != nil {
		t.Fatal(err)
	}
	if returnWithPrefix != "https://nas.example/app/roundpen" {
		t.Fatalf("fnOS return = %q", returnWithPrefix)
	}

	creds, err := svc.CredsForUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Token != "gho_operate" || creds[0].Host != "github.com" || creds[0].Provider != gitcred.ProviderGitHub {
		t.Fatalf("creds %+v", creds)
	}
	script := gitcred.InstallScript(creds, gitcred.Identity{})
	want := base64.StdEncoding.EncodeToString([]byte("export GH_TOKEN=\"gho_operate\"\nexport GITHUB_TOKEN=\"gho_operate\"\n"))
	if !strings.Contains(script, want) {
		t.Fatalf("install script missing github env")
	}

	login, err := svc.ListEnabledProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(login) != 0 {
		t.Fatalf("login providers %+v", login)
	}
	linkable, err := svc.ListLinkProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(linkable) != 1 || linkable[0].ID != CloudGitHubProviderID || !strings.Contains(linkable[0].Scopes, "repo") {
		t.Fatalf("linkable %+v", linkable)
	}
}

func TestCloudGitHubHiddenWhenOwnAppExists(t *testing.T) {
	own, err := Normalize(Provider{
		Kind: KindGitHub, Host: "github.com", ClientID: "cid", ClientSecret: "sec", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Store:       newFakeStore(own),
		GitHubRelay: "https://auth.example",
		Install:     fakeInstall{},
	}
	list, err := svc.ListLinkProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == CloudGitHubProviderID {
			t.Fatal("official relay should yield to the admin GitHub app")
		}
	}
	if _, err := svc.AuthorizeURL(context.Background(), StartInput{
		ProviderID: CloudGitHubProviderID, LinkUser: "alice",
		ConsoleScheme: "https", ConsoleHost: "console.test",
	}); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestRelayJSONRejectsNonOK(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer relay.Close()
	svc := &Service{GitHubRelay: relay.URL, Install: fakeInstall{}, HTTP: relay.Client()}
	err := svc.relayJSON(context.Background(), http.MethodGet, "/tickets/x", nil, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}
