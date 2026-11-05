package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

type recordingHost struct {
	fakeHost
	renames  [][2]string
	copies   [][2]string
	publishes [][2]string
	mkdirs   []string
	deletes  []string
}

func (r *recordingHost) RenameHost(_ context.Context, _ tools.Actor, from, to string) error {
	r.renames = append(r.renames, [2]string{from, to})
	return nil
}

func (r *recordingHost) MkdirHost(_ context.Context, _ tools.Actor, path string) error {
	r.mkdirs = append(r.mkdirs, path)
	return nil
}

func (r *recordingHost) DeleteHost(_ context.Context, _ tools.Actor, path string, _ bool) error {
	r.deletes = append(r.deletes, path)
	return nil
}

func (r *recordingHost) MoveHost(ctx context.Context, actor tools.Actor, from, to string) error {
	return r.RenameHost(ctx, actor, from, to)
}

func (r *recordingHost) CopyHost(_ context.Context, _ tools.Actor, from, to string) error {
	r.copies = append(r.copies, [2]string{from, to})
	return nil
}

func (r *recordingHost) PublishHost(_ context.Context, _ tools.Actor, from, to string) error {
	r.publishes = append(r.publishes, [2]string{from, to})
	return nil
}

func TestHostApplyPlanBatchRename(t *testing.T) {
	host := &recordingHost{}
	reg := tools.NewRegistry()
	tools.RegisterHostWrite(reg, &tools.AgentBinder{Host: host})

	args := json.RawMessage(`{"items":[
		{"op":"rename","from":"/vol1/share/a.txt","to":"/vol1/share/b.txt"},
		{"op":"mkdir","to":"/vol1/share/memories"},
		{"op":"delete","from":"/vol1/share/old.txt"}
	]}`)
	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "HostApplyPlan", args)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.renames) != 1 || host.renames[0][0] != "/vol1/share/a.txt" {
		t.Fatalf("renames=%v", host.renames)
	}
	if len(host.mkdirs) != 1 || host.mkdirs[0] != "/vol1/share/memories" {
		t.Fatalf("mkdirs=%v", host.mkdirs)
	}
	if len(host.deletes) != 1 || host.deletes[0] != "/vol1/share/old.txt" {
		t.Fatalf("deletes=%v", host.deletes)
	}
	if !strings.Contains(out, "Applied 3") {
		t.Fatalf("out=%q", out)
	}
}

func TestHostApplyPlanCapsItems(t *testing.T) {
	host := &recordingHost{}
	reg := tools.NewRegistry()
	tools.RegisterHostWrite(reg, &tools.AgentBinder{Host: host})

	items := make([]map[string]string, tools.MaxHostPlanItems+1)
	for i := range items {
		items[i] = map[string]string{
			"op":   "mkdir",
			"path": fmt.Sprintf("/vol1/share/d%d", i),
		}
	}
	raw, _ := json.Marshal(map[string]any{"items": items})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "HostApplyPlan", raw)
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("err=%v", err)
	}
}

func TestHostRenameWithoutHost(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterHostWrite(reg, &tools.AgentBinder{})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "HostRename",
		json.RawMessage(`{"from":"/vol1/a","to":"/vol1/b"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err=%v", err)
	}
}

func TestHostWriteToolsAreMutating(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterHostWrite(reg, &tools.AgentBinder{Host: &recordingHost{}})
	for _, name := range []string{"HostRename", "HostMove", "HostCopy", "HostMkdir", "HostDelete", "HostApplyPlan"} {
		tool, ok := reg.Get(name)
		if !ok || !tool.Mutating {
			t.Fatalf("%s missing or not mutating", name)
		}
	}
}

func TestHostCopyPublishesWorkspaceSource(t *testing.T) {
	ws := t.TempDir()
	src := filepath.Join(ws, "outputs", "track.flac")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("flac"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &recordingHost{}
	slots := &stubAgentSlots{id: "sb-1"}
	ex := &stubExec{ws: ws}
	reg := tools.NewRegistry()
	tools.RegisterHostWrite(reg, &tools.AgentBinder{Host: host, Slots: slots, Exec: ex})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "HostCopy",
		json.RawMessage(`{"from":"/workspace/outputs/track.flac","to":"/vol2/1000/媒体文件/音乐/track.flac"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(host.publishes) != 1 {
		t.Fatalf("publishes=%v copies=%v", host.publishes, host.copies)
	}
	if host.publishes[0][0] != src {
		t.Fatalf("publish from=%q want %q", host.publishes[0][0], src)
	}
	if host.publishes[0][1] != "/vol2/1000/媒体文件/音乐/track.flac" {
		t.Fatalf("publish to=%q", host.publishes[0][1])
	}
	if !strings.Contains(out, "/workspace/outputs/track.flac") {
		t.Fatalf("out=%q", out)
	}
}

func TestHostCopyHostToHostStillUsesCopyHost(t *testing.T) {
	host := &recordingHost{}
	reg := tools.NewRegistry()
	tools.RegisterHostWrite(reg, &tools.AgentBinder{Host: host})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "HostCopy",
		json.RawMessage(`{"from":"/vol1/share/a.flac","to":"/vol1/share/music/a.flac"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(host.copies) != 1 || len(host.publishes) != 0 {
		t.Fatalf("copies=%v publishes=%v", host.copies, host.publishes)
	}
}
