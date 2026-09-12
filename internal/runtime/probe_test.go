package runtime

import (
	"context"
	"testing"
)

func TestNormalizeAndEngineOfImage(t *testing.T) {
	if NormalizeEngine("QEMU") != EngineQEMU {
		t.Fatal("qemu")
	}
	if NormalizeEngine("docker") != EngineDocker {
		t.Fatal("docker")
	}
	if NormalizeEngine("kern") != "" {
		t.Fatal("kern must be removed")
	}
	if EngineOfImage("images/x.qcow2") != EngineQEMU {
		t.Fatal("qcow2")
	}
	if EngineOfImage("python:3.12") != EngineDocker {
		t.Fatal("oci")
	}
}

func TestSnapshotDockerNotReady(t *testing.T) {
	p := &Probe{Cfg: nil, DockerReady: false, DockerErr: "cannot ping"}
	snap := p.Snapshot()
	if snap.DockerReady {
		t.Fatal("docker should not be ready")
	}
	if len(snap.Missing) == 0 || len(snap.Setup) == 0 {
		t.Fatalf("missing=%v setup=%v", snap.Missing, snap.Setup)
	}
	if snap.AgentImage == "" {
		t.Fatal("agent image ref required")
	}
}

func TestSnapshotDockerReadyImageLocal(t *testing.T) {
	p := &Probe{DockerReady: true, HasImage: func(_ context.Context, _ string) bool { return true }}
	snap := p.Snapshot()
	if !snap.DockerReady || !snap.ImageLocal {
		t.Fatalf("snap=%+v", snap)
	}
	if len(snap.Missing) != 0 || len(snap.Setup) != 0 {
		t.Fatalf("missing=%v setup=%v", snap.Missing, snap.Setup)
	}
}

func TestRequireAgentDockerNotReady(t *testing.T) {
	p := &Probe{DockerReady: false, DockerErr: "no daemon"}
	err := p.RequireAgent("docker")
	n := AsNotReady(err)
	if n == nil || n.Engine != EngineDocker {
		t.Fatalf("err=%v", err)
	}
	if len(n.Setup) == 0 {
		t.Fatal("expected setup")
	}
}
