package preview

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type fakeManager struct {
	sandbox.Manager
	sb *sandbox.Sandbox
}

func (f fakeManager) Get(_ context.Context, id string) (*sandbox.Sandbox, error) {
	if f.sb != nil && f.sb.ID == id {
		return f.sb, nil
	}
	return nil, errors.New("sandbox not found")
}

func newLinkTestServer(t *testing.T, publicURL string) (*httptest.Server, *Handler) {
	t.Helper()
	h := &Handler{
		Manager:   fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}},
		Tokens:    NewStore(time.Minute),
		PublicURL: publicURL,
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func fetchLink(t *testing.T, srv *httptest.Server) (*http.Response, previewLinkResp) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/v1/sandboxes/sb-1/preview-link?port=3000")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var link previewLinkResp
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatal(err)
	}
	return resp, link
}

func TestPreviewLinkSameOriginUsesCookie(t *testing.T) {
	srv, h := newLinkTestServer(t, "")
	h.PublicURL = srv.URL // same host as the request

	resp, link := fetchLink(t, srv)
	if strings.Contains(link.URL, "token=") {
		t.Fatalf("same-origin URL must not carry the token: %s", link.URL)
	}
	if link.Token == "" {
		t.Fatal("token missing from response body")
	}

	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "roundpen_preview" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("roundpen_preview cookie not set")
	}
	if cookie.Value != link.Token {
		t.Fatalf("cookie value != token")
	}
	if !cookie.HttpOnly {
		t.Fatal("cookie must be HttpOnly")
	}
	if cookie.Path != "/p/sb-1/3000" {
		t.Fatalf("cookie path = %q", cookie.Path)
	}
}

func TestPreviewLinkCrossOriginKeepsURLToken(t *testing.T) {
	srv, _ := newLinkTestServer(t, "https://preview.example.com")

	resp, link := fetchLink(t, srv)
	if !strings.Contains(link.URL, "token="+link.Token) {
		t.Fatalf("cross-origin URL must carry the token: %s", link.URL)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "roundpen_preview" {
			t.Fatal("cookie on the console domain is useless for a separate preview origin")
		}
	}
}

func TestTokenIssueAndLookup(t *testing.T) {
	s := NewStore(time.Minute)
	tok, exp, err := s.Issue("sb-1", 3000, "alice")
	if err != nil || tok == "" {
		t.Fatalf("issue: %v %q", err, tok)
	}
	if exp.Before(time.Now()) {
		t.Fatal("expires in the past")
	}
	sid, port, owner, ok := s.Lookup(tok)
	if !ok || sid != "sb-1" || port != 3000 || owner != "alice" {
		t.Fatalf("lookup: ok=%v sid=%s port=%d owner=%s", ok, sid, port, owner)
	}
	if _, _, _, ok := s.Lookup("nope"); ok {
		t.Fatal("expected miss")
	}
}

func TestTokenExpiry(t *testing.T) {
	s := NewStore(10 * time.Millisecond)
	tok, _, err := s.Issue("sb-1", 80, "alice")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, _, ok := s.Lookup(tok); ok {
		t.Fatal("expected expired")
	}
}
