package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// fakeRegistry is an in-memory ClaimRegistry.
type fakeRegistry struct {
	claims map[string]Claim
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{claims: map[string]Claim{}}
}

func (f *fakeRegistry) Resolve(_ context.Context, name string) (string, int, bool) {
	c, ok := f.claims[name]
	if !ok {
		return "", 0, false
	}
	return c.SandboxID, c.Port, true
}

func (f *fakeRegistry) Get(_ context.Context, name string) (*Claim, error) {
	c, ok := f.claims[name]
	if !ok {
		return nil, fmt.Errorf("no claim %q", name)
	}
	return &c, nil
}

func (f *fakeRegistry) List(_ context.Context, owner string) ([]Claim, error) {
	var out []Claim
	for _, c := range f.claims {
		if owner == "" || c.Owner == owner {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeRegistry) Claim(_ context.Context, c Claim) error {
	if held, ok := f.claims[c.Name]; ok && held.Owner != c.Owner {
		return ErrNameTaken
	}
	if _, ok := f.claims[c.Name]; !ok {
		c.CreatedAt = time.Now()
	}
	f.claims[c.Name] = c
	return nil
}

func (f *fakeRegistry) Release(_ context.Context, name, owner string) (bool, error) {
	held, ok := f.claims[name]
	if !ok || (owner != "" && held.Owner != owner) {
		return false, nil
	}
	delete(f.claims, name)
	return true, nil
}

// newClaimServer mounts the preview routes behind a user-injecting wrapper, so
// tests exercise the same handlers the daemon serves.
func newClaimServer(t *testing.T, h *Handler, user *storage.User) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != nil {
			r = r.WithContext(auth.WithUser(r.Context(), user))
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postClaim(t *testing.T, srv *httptest.Server, body string) (*http.Response, claimResp) {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL+"/v1/preview-domains", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	var out claimResp
	if resp.StatusCode == http.StatusCreated {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp, out
}

func TestValidateLabel(t *testing.T) {
	for _, name := range []string{"demo", "my-app", "app2", "a"} {
		if err := ValidateLabel(name); err != nil {
			t.Fatalf("%q must be claimable: %v", name, err)
		}
	}
	for _, name := range []string{
		"", "-app", "app-", "My-App", "my_app", "a.b", "www", "api", "console",
		strings.Repeat("a", 64), "app!", "http://app",
	} {
		if err := ValidateLabel(name); err == nil {
			t.Fatalf("%q must be refused", name)
		}
	}
}

func TestClaimDomainBooksNameAndReturnsLink(t *testing.T) {
	registry := newFakeRegistry()
	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}},
		Tokens:  NewStore(time.Minute),
		Domain:  "preview.test",
		Scheme:  "https",
		Claims:  registry,
	}
	srv := newClaimServer(t, h, &storage.User{Username: "alice", Role: storage.RoleUser})

	resp, claim := postClaim(t, srv, `{"name":"Demo","sandboxID":"sb-1","port":3000}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if claim.Name != "demo" || claim.Address != "demo.preview.test" || claim.Owner != "alice" {
		t.Fatalf("claim = %+v", claim)
	}
	if !strings.HasPrefix(claim.URL, "https://demo.preview.test/?token=") {
		t.Fatalf("url = %q", claim.URL)
	}
	if token := strings.TrimPrefix(claim.URL, "https://demo.preview.test/?token="); token == "" {
		t.Fatal("token missing from the url")
	}
	// The token belongs to the claimed target, so the proxy would accept it.
	if _, _, ok := registry.Resolve(context.Background(), "demo"); !ok {
		t.Fatal("claim was not recorded")
	}
}

func TestClaimDomainRejectsTakenAndForeignNames(t *testing.T) {
	registry := newFakeRegistry()
	_ = registry.Claim(context.Background(), Claim{Name: "demo", SandboxID: "sb-2", Port: 3000, Owner: "bob"})
	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}},
		Tokens:  NewStore(time.Minute),
		Domain:  "preview.test",
		Claims:  registry,
	}
	srv := newClaimServer(t, h, &storage.User{Username: "alice", Role: storage.RoleUser})

	if resp, _ := postClaim(t, srv, `{"name":"demo","sandboxID":"sb-1","port":3000}`); resp.StatusCode != http.StatusConflict {
		t.Fatalf("taken name: status = %d, want 409", resp.StatusCode)
	}
	if resp, _ := postClaim(t, srv, `{"name":"mine","sandboxID":"nope","port":3000}`); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown workspace: status = %d, want 404", resp.StatusCode)
	}
	if resp, _ := postClaim(t, srv, `{"name":"mine","sandboxID":"sb-1","port":0}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad port: status = %d, want 400", resp.StatusCode)
	}
	if resp, _ := postClaim(t, srv, `{"name":"www","sandboxID":"sb-1","port":3000}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reserved name: status = %d, want 400", resp.StatusCode)
	}

	// Somebody else's workspace is not claimable, token or not.
	other := &Handler{Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "bob"}}, Tokens: NewStore(time.Minute), Domain: "preview.test", Claims: newFakeRegistry()}
	otherSrv := newClaimServer(t, other, &storage.User{Username: "alice", Role: storage.RoleUser})
	if resp, _ := postClaim(t, otherSrv, `{"name":"mine","sandboxID":"sb-1","port":3000}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign workspace: status = %d, want 403", resp.StatusCode)
	}
}

func TestClaimDomainWithoutZoneExplainsItself(t *testing.T) {
	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}},
		Tokens:  NewStore(time.Minute),
	}
	srv := newClaimServer(t, h, &storage.User{Username: "alice", Role: storage.RoleUser})
	resp, _ := postClaim(t, srv, `{"name":"demo","sandboxID":"sb-1","port":3000}`)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if resp, _ := postClaim(t, newClaimServer(t, h, nil), `{"name":"demo","sandboxID":"sb-1","port":3000}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no user: status = %d, want 401", resp.StatusCode)
	}
}

func TestVhostRouterServesClaimedNames(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "host=%s path=%s", r.Host, r.URL.Path)
	}))
	defer upstream.Close()

	registry := newFakeRegistry()
	_ = registry.Claim(context.Background(), Claim{Name: "demo", SandboxID: "sb-1", Port: 3000, Owner: "alice"})
	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}, addr: upstream.Listener.Addr().String()},
		Tokens:  NewStore(time.Minute),
		Domain:  "preview.test",
		Claims:  registry,
	}
	token, _, err := h.Tokens.Issue("sb-1", 3000, "alice")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.VhostRouter(consoleStub()))
	defer srv.Close()

	resp := vhostGet(t, srv, "demo.preview.test", "/assets/app.js", &http.Cookie{Name: "roundpen_preview", Value: token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body := readBody(t, resp); body != "host=demo.preview.test path=/assets/app.js" {
		t.Fatalf("upstream saw %s", body)
	}

	// The same token does not open a claimed name pointing elsewhere.
	foreign := newFakeRegistry()
	_ = foreign.Claim(context.Background(), Claim{Name: "other", SandboxID: "sb-2", Port: 3000, Owner: "bob"})
	h.Claims = foreign
	if resp := vhostGet(t, srv, "other.preview.test", "/", &http.Cookie{Name: "roundpen_preview", Value: token}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("foreign claim: status = %d, want 401", resp.StatusCode)
	}
}
