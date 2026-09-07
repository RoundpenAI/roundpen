package runtime

import "testing"

func TestNormalizeAndTemplate(t *testing.T) {
	if NormalizeEngine("QEMU") != EngineQEMU {
		t.Fatal("qemu")
	}
	if TemplateForEngine("docker") != "code-agent" {
		t.Fatal("docker template")
	}
	if TemplateForEngine("kern") != "host" {
		t.Fatal("kern template")
	}
	if TemplateForEngine("qemu") != "agent-claude" {
		t.Fatal("qemu template")
	}
	if EngineOfImage("images/x.qcow2") != EngineQEMU {
		t.Fatal("qcow2")
	}
	if EngineOfImage("host") != EngineKern {
		t.Fatal("host")
	}
	if EngineOfImage("python:3.12") != EngineDocker {
		t.Fatal("oci")
	}
}

func TestProbeKernWithoutBwrapStillReports(t *testing.T) {
	p := &Probe{Cfg: nil, DockerReady: false, DockerErr: "cannot ping"}
	snap := p.Snapshot()
	if len(snap.Engines) != 3 {
		t.Fatalf("engines=%d", len(snap.Engines))
	}
	if snap.DefaultAgentEngine != EngineQEMU {
		t.Fatalf("default %s", snap.DefaultAgentEngine)
	}
	var docker EngineStatus
	for _, e := range snap.Engines {
		if e.ID == EngineDocker {
			docker = e
		}
	}
	if docker.AgentReady {
		t.Fatal("docker should not be ready")
	}
	if len(docker.Setup) == 0 {
		t.Fatal("docker setup steps")
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
