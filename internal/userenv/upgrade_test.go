package userenv

import (
	"context"
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func newUpgradeService(boxes *fakeSandboxes) (*Service, *memSlots) {
	slots := &memSlots{}
	return &Service{Store: slots, Sandboxes: boxes}, slots
}

func TestUpgradeAgent_UpToDateSkipsRebuild(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: false, refreshDigest: "sha256:same"}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning, Image: "img:1"})
	svc, _ := newUpgradeService(boxes)

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "up_to_date" || res.Digest != "sha256:same" {
		t.Fatalf("res=%+v", res)
	}
	if boxes.deletes != 0 || boxes.creates != 0 {
		t.Fatalf("expected no rebuild: deletes=%d creates=%d", boxes.deletes, boxes.creates)
	}
	if res.Environment.Slot != SlotAgent {
		t.Fatalf("environment=%+v", res.Environment)
	}
	if boxes.refreshRef != "code-agent" {
		t.Fatalf("refresh ref=%q", boxes.refreshRef)
	}
}

func TestUpgradeAgent_RebuildsWhenChanged(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true, refreshImage: "img:2", refreshDigest: "sha256:new"}
	mapped := &sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning}
	boxes.put(mapped)
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "upgraded" {
		t.Fatalf("status=%q", res.Status)
	}
	if boxes.deletes != 1 || boxes.creates != 1 {
		t.Fatalf("expected one rebuild: deletes=%d creates=%d", boxes.deletes, boxes.creates)
	}
	if res.Image != "img:2" {
		t.Fatalf("image=%q", res.Image)
	}
}

func TestUpgradeAgent_ForceRebuildsEvenWhenUnchanged(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: false}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning})
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.UpgradeAgent(ctx, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "restarted" || boxes.deletes != 1 || boxes.creates != 1 {
		t.Fatalf("res=%+v deletes=%d creates=%d", res, boxes.deletes, boxes.creates)
	}
	if boxes.refreshes != 1 {
		t.Fatalf("force should still refresh the image first: refreshes=%d", boxes.refreshes)
	}
}

func TestUpgradeAgent_RefreshFailureKeepsSandbox(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshErr: errors.New("registry unreachable")}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning})
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	_, err := svc.UpgradeAgent(ctx, "alice", true)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if boxes.deletes != 0 || boxes.creates != 0 {
		t.Fatalf("sandbox must be untouched: deletes=%d creates=%d", boxes.deletes, boxes.creates)
	}
	if _, ok := boxes.byID["agent-1"]; !ok {
		t.Fatal("existing sandbox was removed")
	}
}

func TestUpgradeAgent_CreatesWhenAbsent(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true}
	svc, _ := newUpgradeService(boxes)

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "created" || boxes.creates != 1 || boxes.deletes != 0 {
		t.Fatalf("res=%+v deletes=%d creates=%d", res, boxes.deletes, boxes.creates)
	}
}

func TestUpgradeAgent_DeletesUnmappedExistingSandbox(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true, refreshImage: "img:2"}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning, Image: "img:1"})
	svc, _ := newUpgradeService(boxes) // no mapping exists

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "upgraded" || boxes.deletes != 1 || boxes.creates != 1 {
		t.Fatalf("res=%+v deletes=%d creates=%d", res, boxes.deletes, boxes.creates)
	}
	if res.Image != "img:2" {
		t.Fatalf("image=%q", res.Image)
	}
}

func TestUpgradeAgent_DeleteFailureAborts(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true, deleteErr: errors.New("docker daemon down")}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning})
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	_, err := svc.UpgradeAgent(ctx, "alice", false)
	if err == nil {
		t.Fatal("expected delete error")
	}
	if boxes.creates != 0 {
		t.Fatalf("must abort before rebuild: creates=%d", boxes.creates)
	}
	if _, ok := boxes.byID["agent-1"]; !ok {
		t.Fatal("existing sandbox was removed")
	}
}

func TestUpgradeAgent_StaleMappingStillCreates(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true, deleteErr: sandbox.ErrNotFound}
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}
	// The mapping points at a sandbox that no longer exists.

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "created" || boxes.creates != 1 {
		t.Fatalf("res=%+v creates=%d", res, boxes.creates)
	}
}
