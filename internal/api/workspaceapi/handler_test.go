package workspaceapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
