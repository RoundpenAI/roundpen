package config

import "testing"

func TestParseSSHURL(t *testing.T) {
	tg, err := ParseSSHURL("ssh://mike@10.10.1.5")
	if err != nil {
		t.Fatal(err)
	}
	if tg.User != "mike" || tg.Host != "10.10.1.5" {
		t.Fatalf("%+v", tg)
	}
	if tg.String() != "mike@10.10.1.5" {
		t.Fatalf("string=%s", tg.String())
	}
}

func TestEffectiveDataRootRemote(t *testing.T) {
	cfg := &Config{Backend: "docker", DockerHost: "ssh://mike@10.10.1.5", DataRoot: "./data"}
	if got := cfg.EffectiveDataRoot(); got != "/var/lib/roundpen" {
		t.Fatalf("got %s", got)
	}
	cfg.DataRoot = "/data/roundpen"
	if got := cfg.EffectiveDataRoot(); got != "/data/roundpen" {
		t.Fatalf("got %s", got)
	}
}

func TestWorkspaceUsesSSH(t *testing.T) {
	cfg := &Config{Backend: "docker", DockerHost: "ssh://mike@10.10.1.5"}
	if !cfg.WorkspaceUsesSSH() {
		t.Fatal("expected ssh workspace")
	}
	cfg.DockerHost = "unix:///var/run/docker.sock"
	if cfg.WorkspaceUsesSSH() {
		t.Fatal("expected local workspace")
	}
}
