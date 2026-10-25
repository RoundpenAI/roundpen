package vcs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListGitHubRepos(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		page++
		if page == 1 {
			w.Write([]byte(`[
				{"name":"a","full_name":"me/a","clone_url":"https://github.com/me/a.git",
				 "html_url":"https://github.com/me/a","private":false,"description":"x",
				 "updated_at":"2026-01-01T00:00:00Z","owner":{"login":"me"}}
			]`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	// Patch API base by using a custom client that rewrites — instead, call
	// ListGitHubRepos against real URL won't hit our server. Unit-test via
	// a thin wrapper would need injection; keep NormalizeCloneURL covered and
	// smoke List with a redirected transport.
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})}
	repos, err := ListGitHubRepos(context.Background(), client, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "me/a" {
		t.Fatalf("repos=%+v", repos)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNormalizeCloneURL(t *testing.T) {
	cases := map[string]string{
		"me/app":                         "https://github.com/me/app.git",
		"https://github.com/me/app.git":  "https://github.com/me/app.git",
		"":                               "",
	}
	for in, want := range cases {
		if got := NormalizeCloneURL(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}
