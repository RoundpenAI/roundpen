package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

type fakeHost struct {
	content string
	isDir   bool
	files   []string
	opened  string
	glob    struct {
		root    string
		pattern string
		limit   int
	}
}

func (f *fakeHost) OpenHost(_ context.Context, _ tools.Actor, absPath string) (io.ReadCloser, os.FileInfo, error) {
	f.opened = absPath
	return io.NopCloser(bytes.NewReader([]byte(f.content))), fakeInfo{isDir: f.isDir}, nil
}

func (f *fakeHost) GlobHost(_ context.Context, _ tools.Actor, rootAbs, pattern string, limit int) ([]string, error) {
	f.glob.root, f.glob.pattern, f.glob.limit = rootAbs, pattern, limit
	return f.files, nil
}

type fakeInfo struct{ isDir bool }

func (f fakeInfo) Name() string       { return "note.md" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.isDir }
func (f fakeInfo) Sys() any           { return nil }

func TestReadFromGrantedHostPath(t *testing.T) {
	host := &fakeHost{content: "alpha\nbeta\n"}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Host: host})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read",
		json.RawMessage(`{"file_path":"/vol1/share/note.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if host.opened != "/vol1/share/note.md" {
		t.Fatalf("host opened %q", host.opened)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "     2|beta") {
		t.Fatalf("out=%q", out)
	}
}

func TestReadRelativePathStillUsesWorkspace(t *testing.T) {
	host := &fakeHost{content: "host\n"}
	files := &memFiles{data: map[string][]byte{"a.md": []byte("workspace\n")}}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "sbx"}, Files: files, Host: host})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read",
		json.RawMessage(`{"file_path":"a.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "workspace") || host.opened != "" {
		t.Fatalf("out=%q opened=%q", out, host.opened)
	}
}

func TestReadHostPathWithoutHostSupport(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{})

	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read",
		json.RawMessage(`{"file_path":"/vol1/share/note.md"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadHostDirectoryPointsAtGlob(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Host: &fakeHost{isDir: true}})

	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read",
		json.RawMessage(`{"file_path":"/vol1/share"}`))
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadRefusesReservedHostPaths(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Host: &fakeHost{content: "x"}})

	for _, p := range []string{"/", "/proc/self/environ", "/sys/kernel/notes", "/dev/sda"} {
		_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read",
			json.RawMessage(fmt.Sprintf(`{"file_path":%q}`, p)))
		if err == nil {
			t.Fatalf("%s must be refused", p)
		}
	}
}

func TestGlobGrantedHostDir(t *testing.T) {
	host := &fakeHost{files: []string{"/vol1/share/a.md", "/vol1/share/b.md"}}
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{Host: host})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Glob",
		json.RawMessage(`{"pattern":"**/*.md","path":"/vol1/./share/"}`))
	if err != nil {
		t.Fatal(err)
	}
	if host.glob.root != "/vol1/share" || host.glob.pattern != "**/*.md" {
		t.Fatalf("glob args: %+v", host.glob)
	}
	if host.glob.limit != tools.MaxHostGlobResults {
		t.Fatalf("limit=%d", host.glob.limit)
	}
	if !strings.Contains(out, "/vol1/share/a.md") || !strings.Contains(out, "/vol1/share/b.md") {
		t.Fatalf("out=%q", out)
	}
}

func TestGlobHostTruncationHint(t *testing.T) {
	host := &fakeHost{files: make([]string, tools.MaxHostGlobResults)}
	for i := range host.files {
		host.files[i] = fmt.Sprintf("/vol1/share/f%03d.md", i)
	}
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{Host: host})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Glob",
		json.RawMessage(`{"pattern":"*.md","path":"/vol1/share"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "narrow the pattern") {
		t.Fatalf("missing truncation hint: %q", out)
	}
}

func TestGlobHostWithoutHostSupport(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{})

	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Glob",
		json.RawMessage(`{"pattern":"*.md","path":"/vol1/share"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err=%v", err)
	}
}
