package workspaceapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

type fakeEnvs struct {
	sb  *sandbox.Sandbox
	err error
}

func (f *fakeEnvs) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sb, nil
}

type fakeFiles struct {
	entries map[string][]byte
}

func (f *fakeFiles) ListGuestFiles(_ context.Context, _, rel string) ([]workspace.DirEntry, error) {
	if strings.Contains(rel, "..") {
		return nil, errEscape
	}
	var out []workspace.DirEntry
	prefix := rel
	if prefix == "." {
		prefix = ""
	}
	seen := map[string]bool{}
	for k := range f.entries {
		name := k
		if prefix != "" {
			if !strings.HasPrefix(k, prefix+"/") {
				continue
			}
			name = strings.TrimPrefix(k, prefix+"/")
		}
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, workspace.DirEntry{Name: name, IsDir: true})
			continue
		}
		out = append(out, workspace.DirEntry{Name: name, Size: int64(len(f.entries[k]))})
	}
	return out, nil
}

func (f *fakeFiles) ReadGuestFile(_ context.Context, _, rel string) (io.ReadCloser, error) {
	b, ok := f.entries[rel]
	if !ok {
		return nil, osNotExist
	}
	return io.NopCloser(strings.NewReader(string(b))), nil
}

func (f *fakeFiles) WriteGuestFile(_ context.Context, _, rel string, r io.Reader) error {
	if strings.Contains(rel, "..") {
		return errEscape
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if f.entries == nil {
		f.entries = map[string][]byte{}
	}
	f.entries[rel] = b
	return nil
}

func (f *fakeFiles) RemoveGuestFile(_ context.Context, _, rel string) error {
	if _, ok := f.entries[rel]; !ok {
		return osNotExist
	}
	delete(f.entries, rel)
	return nil
}

func (f *fakeFiles) MoveGuestFile(_ context.Context, _, src, dest string) error {
	return f.relocate(src, dest, false)
}

func (f *fakeFiles) CopyGuestFile(_ context.Context, _, src, dest string) error {
	return f.relocate(src, dest, true)
}

// relocate mirrors the service contract: an existing directory receives the
// entry under its own name, and an occupied target conflicts.
func (f *fakeFiles) relocate(src, dest string, isCopy bool) error {
	if strings.Contains(src, "..") || strings.Contains(dest, "..") {
		return errEscape
	}
	b, ok := f.entries[src]
	if !ok {
		return osNotExist
	}
	if f.isDir(dest) {
		dest = dest + "/" + path.Base(src)
	}
	if _, exists := f.entries[dest]; exists {
		return sandbox.ErrConflict
	}
	f.entries[dest] = b
	if !isCopy {
		delete(f.entries, src)
	}
	return nil
}

func (f *fakeFiles) isDir(rel string) bool {
	prefix := strings.TrimSuffix(rel, "/") + "/"
	for k := range f.entries {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

var (
	errEscape  = errString("path escapes workspace")
	osNotExist = errString("no such file")
)

type errString string

func (e errString) Error() string { return string(e) }

func withUser(r *http.Request, username string) *http.Request {
	ctx := auth.WithUser(r.Context(), &storage.User{Username: username, Role: "user"})
	return r.WithContext(ctx)
}

func TestUnauthorized(t *testing.T) {
	h := &Handler{Envs: &fakeEnvs{sb: &sandbox.Sandbox{ID: "alice"}}, Files: &fakeFiles{}}
	mux := http.NewServeMux()
	h.Mount(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/me/workspace/files?path=.", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestListWriteDelete(t *testing.T) {
	files := &fakeFiles{entries: map[string][]byte{}}
	h := &Handler{
		Envs:  &fakeEnvs{sb: &sandbox.Sandbox{ID: "alice"}},
		Files: files,
	}
	mux := http.NewServeMux()
	h.Mount(mux)

	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/me/workspace/files?path=hello.txt", strings.NewReader("hi")), "alice")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("write status %d body %s", rr.Code, rr.Body.String())
	}

	req = withUser(httptest.NewRequest(http.MethodGet, "/v1/me/workspace/files?path=.", nil), "alice")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "hello.txt") {
		t.Fatalf("list %d %s", rr.Code, rr.Body.String())
	}

	req = withUser(httptest.NewRequest(http.MethodDelete, "/v1/me/workspace/files?path=hello.txt", nil), "alice")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete %d", rr.Code)
	}
}

func TestPathEscape(t *testing.T) {
	h := &Handler{
		Envs:  &fakeEnvs{sb: &sandbox.Sandbox{ID: "alice"}},
		Files: &fakeFiles{entries: map[string][]byte{}},
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/me/workspace/files?path=../x", strings.NewReader("x")), "alice")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestMoveCopyEndpoints(t *testing.T) {
	files := &fakeFiles{entries: map[string][]byte{
		"a.txt":     []byte("hi"),
		"sub/b.txt": []byte("x"),
	}}
	h := &Handler{Envs: &fakeEnvs{sb: &sandbox.Sandbox{ID: "alice"}}, Files: files}
	mux := http.NewServeMux()
	h.Mount(mux)

	// Rename in place: dest is a literal name, not a directory.
	req := withUser(httptest.NewRequest(http.MethodPost,
		"/v1/me/workspace/files/move?path=a.txt", strings.NewReader(`{"dest":"renamed.txt"}`)), "alice")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("rename %d %s", rr.Code, rr.Body.String())
	}
	if _, ok := files.entries["renamed.txt"]; !ok {
		t.Fatal("renamed file missing")
	}
	if _, ok := files.entries["a.txt"]; ok {
		t.Fatal("source must be gone after move")
	}

	// Copy into an existing directory keeps the entry's own name.
	req = withUser(httptest.NewRequest(http.MethodPost,
		"/v1/me/workspace/files/copy?path=renamed.txt", strings.NewReader(`{"dest":"sub"}`)), "alice")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("copy %d %s", rr.Code, rr.Body.String())
	}
	if _, ok := files.entries["sub/renamed.txt"]; !ok {
		t.Fatal("copied file missing")
	}
	if _, ok := files.entries["renamed.txt"]; !ok {
		t.Fatal("copy must keep the source")
	}

	// Occupied target → 409.
	req = withUser(httptest.NewRequest(http.MethodPost,
		"/v1/me/workspace/files/copy?path=renamed.txt", strings.NewReader(`{"dest":"sub/renamed.txt"}`)), "alice")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("conflict %d %s", rr.Code, rr.Body.String())
	}

	// Missing source → 404.
	req = withUser(httptest.NewRequest(http.MethodPost,
		"/v1/me/workspace/files/move?path=ghost.txt", strings.NewReader(`{"dest":"x"}`)), "alice")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing %d %s", rr.Code, rr.Body.String())
	}

	// Bad body / empty dest → 400.
	for _, body := range []string{"not json", `{"dest":"  "}`} {
		req = withUser(httptest.NewRequest(http.MethodPost,
			"/v1/me/workspace/files/move?path=renamed.txt", strings.NewReader(body)), "alice")
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("body %q → %d %s", body, rr.Code, rr.Body.String())
		}
	}
}

func TestReadContentTypeAndDownload(t *testing.T) {
	files := &fakeFiles{entries: map[string][]byte{
		"pic.png":   []byte("PNG"),
		"note.md":   []byte("# hi"),
		"page.html": []byte("<script>x()</script>"),
		"blob.bin":  []byte{0, 1, 2},
	}}
	h := &Handler{Envs: &fakeEnvs{sb: &sandbox.Sandbox{ID: "alice"}}, Files: files}
	mux := http.NewServeMux()
	h.Mount(mux)

	cases := []struct{ path, wantCT string }{
		{"pic.png", "image/png"},
		{"note.md", "text/plain; charset=utf-8"},
		// HTML never renders as a document on the console origin.
		{"page.html", "text/plain; charset=utf-8"},
		{"blob.bin", "application/octet-stream"},
	}
	for _, c := range cases {
		req := withUser(httptest.NewRequest(http.MethodGet,
			"/v1/me/workspace/files/content?path="+c.path, nil), "alice")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != c.wantCT {
			t.Fatalf("%s: %d ct=%q", c.path, rr.Code, rr.Header().Get("Content-Type"))
		}
		if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: nosniff missing", c.path)
		}
	}

	req := withUser(httptest.NewRequest(http.MethodGet,
		"/v1/me/workspace/files/content?path=note.md&download=1", nil), "alice")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	disp := rr.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "attachment") || !strings.Contains(disp, "note.md") {
		t.Fatalf("download disposition %q", disp)
	}
}
