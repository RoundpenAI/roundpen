package oauth

import (
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeProviderDefaults(t *testing.T) {
	cases := []struct {
		name    string
		in      Provider
		wantID  string
		wantURL [3]string // auth, token, api
	}{
		{
			name:    "github.com",
			in:      Provider{Kind: "github", Host: "github.com", ClientID: "cid", ClientSecret: "sec", Enabled: true},
			wantID:  "github",
			wantURL: [3]string{"https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", "https://api.github.com"},
		},
		{
			name:    "gitea instance with port",
			in:      Provider{Kind: "gitea", Scheme: "http", Host: "git.eaxi.com:3000", ClientID: "cid", ClientSecret: "sec", Enabled: true},
			wantID:  "gitea-git-eaxi-com-3000",
			wantURL: [3]string{"http://git.eaxi.com:3000/login/oauth/authorize", "http://git.eaxi.com:3000/login/oauth/access_token", "http://git.eaxi.com:3000/api/v1"},
		},
		{
			name:    "github enterprise",
			in:      Provider{Kind: "github", Host: "ghe.corp.test", ClientID: "cid", ClientSecret: "sec", Enabled: true},
			wantID:  "github-ghe-corp-test",
			wantURL: [3]string{"https://ghe.corp.test/login/oauth/authorize", "https://ghe.corp.test/login/oauth/access_token", "https://ghe.corp.test/api/v3"},
		},
		{
			name:    "host pasted as url, explicit endpoints win",
			in:      Provider{Kind: "gitea", Host: "https://Git.Eaxi.com/", AuthURL: "https://sso.test/authorize", ClientID: "cid", ClientSecret: "sec", Enabled: true},
			wantID:  "gitea-git-eaxi-com",
			wantURL: [3]string{"https://sso.test/authorize", "https://git.eaxi.com/login/oauth/access_token", "https://git.eaxi.com/api/v1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.in)
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got.ID != tc.wantID {
				t.Errorf("id = %q, want %q", got.ID, tc.wantID)
			}
			if got.AuthURL != tc.wantURL[0] || got.TokenURL != tc.wantURL[1] || got.APIURL != tc.wantURL[2] {
				t.Errorf("urls = %q / %q / %q, want %q / %q / %q",
					got.AuthURL, got.TokenURL, got.APIURL, tc.wantURL[0], tc.wantURL[1], tc.wantURL[2])
			}
			if got.Scopes != DefaultScopes {
				t.Errorf("scopes = %q, want default %q", got.Scopes, DefaultScopes)
			}
			if got.Label == "" {
				t.Error("label should default to a display name")
			}
		})
	}
}

func TestNormalizeProviderRejects(t *testing.T) {
	cases := []struct {
		name string
		in   Provider
		want string
	}{
		{"unknown kind", Provider{Kind: "gitlab", Host: "gitlab.com"}, "kind"},
		{"empty host", Provider{Kind: "gitea", Host: "  "}, "host"},
		{"bad scheme", Provider{Kind: "gitea", Host: "git.test", Scheme: "ftp"}, "scheme"},
		{"enabled without client id", Provider{Kind: "gitea", Host: "git.test", Enabled: true}, "client id"},
		{"enabled without client secret", Provider{Kind: "gitea", Host: "git.test", ClientID: "cid", Enabled: true}, "client secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(tc.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestProviderCallbackAndAuthorizeURL(t *testing.T) {
	p, err := Normalize(Provider{Kind: "gitea", Host: "git.eaxi.com", ClientID: "cid", ClientSecret: "sec", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.CallbackURL("https", "console.test"), "https://console.test/v1/auth/oauth/gitea-git-eaxi-com/callback"; got != want {
		t.Errorf("callback = %q, want %q", got, want)
	}

	raw := p.AuthorizeURL("st4te", "ch4llenge", "https://console.test/v1/auth/oauth/gitea-git-eaxi-com/callback")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	if u.Scheme != "https" || u.Host != "git.eaxi.com" || u.Path != "/login/oauth/authorize" {
		t.Errorf("authorize base = %s://%s%s", u.Scheme, u.Host, u.Path)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"client_id":             "cid",
		"response_type":         "code",
		"state":                 "st4te",
		"code_challenge":        "ch4llenge",
		"code_challenge_method": "S256",
		"scope":                 DefaultScopes,
		"redirect_uri":          "https://console.test/v1/auth/oauth/gitea-git-eaxi-com/callback",
	} {
		if q.Get(key) != want {
			t.Errorf("authorize %s = %q, want %q", key, q.Get(key), want)
		}
	}
}
