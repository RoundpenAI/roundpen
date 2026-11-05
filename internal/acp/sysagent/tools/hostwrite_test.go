package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

type recordingHost struct {
	fakeHost
	renames [][2]string
	mkdirs  []string
	deletes []string
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

func (r *recordingHost) CopyHost(context.Context, tools.Actor, string, string) error {
	return fmt.Errorf("copy not used")
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
