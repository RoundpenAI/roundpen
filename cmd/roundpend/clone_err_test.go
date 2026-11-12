package main

import (
	"strings"
	"testing"
)

func TestHumanizeCloneErrRewritesUIDNoise(t *testing.T) {
	err := humanizeCloneErr("gitlab-bj.realbtec.cn", "Cloning into '/workspace/projects/reabtec'...\nNo user exists for uid 980\r\nfatal: Could not read from remote repository.")
	got := err.Error()
	if strings.Contains(got, "uid 980") {
		t.Fatalf("still exposes uid noise: %s", got)
	}
	if !strings.Contains(got, "gitlab-bj.realbtec.cn") || !strings.Contains(got, "Git personal tokens") {
		t.Fatalf("missing actionable hint: %s", got)
	}
}

func TestHumanizeCloneErrAuth(t *testing.T) {
	err := humanizeCloneErr("git.example.com", "fatal: could not read Username for 'http://git.example.com': terminal prompts disabled")
	if !strings.Contains(err.Error(), "Git auth to git.example.com failed") {
		t.Fatalf("%s", err)
	}
}
