package project

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNormalizeSubKindAndRepo(t *testing.T) {
	k, err := NormalizeSubKind("pr")
	if err != nil || k != SubKindGitHubPR {
		t.Fatalf("kind: %q %v", k, err)
	}
	k, err = NormalizeSubKind("workflow_run")
	if err != nil || k != SubKindGitHubCI {
		t.Fatalf("ci kind: %q %v", k, err)
	}
	if _, err := NormalizeSubKind("slack"); err == nil {
		t.Fatal("expected error")
	}
	repo, err := NormalizeRepo("https://github.com/Acme/Demo.git")
	if err != nil || repo != "Acme/Demo" {
		t.Fatalf("repo: %q %v", repo, err)
	}
}

func TestVerifyGitHubSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := "s3cret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifyGitHubSignature(secret, body, sig) {
		t.Fatal("expected valid")
	}
	if VerifyGitHubSignature(secret, body, "sha256=deadbeef") {
		t.Fatal("expected invalid")
	}
}

func TestParseGitHubWebhookPR(t *testing.T) {
	body := []byte(`{
		"action":"opened",
		"number":12,
		"pull_request":{"number":12,"title":"Fix","state":"open","html_url":"https://github.com/a/b/pull/12"},
		"repository":{"full_name":"a/b"}
	}`)
	ev := ParseGitHubWebhook("pull_request", body)
	if !ev.Relevant || ev.Repo != "a/b" || ev.Action != "opened" {
		t.Fatalf("%+v", ev)
	}
	if !EventMatchesKind(SubKindGitHubPR, "pull_request") {
		t.Fatal("pr match")
	}
	if EventMatchesKind(SubKindGitHubPR, "check_run") {
		t.Fatal("pr should not match ci")
	}
}

func TestParseGitHubWebhookCI(t *testing.T) {
	body := []byte(`{
		"action":"completed",
		"check_run":{"name":"test","status":"completed","conclusion":"success","html_url":"https://example.com"},
		"repository":{"full_name":"a/b"}
	}`)
	ev := ParseGitHubWebhook("check_run", body)
	if ev.Status != "success" {
		t.Fatalf("status=%q summary=%q", ev.Status, ev.Summary)
	}
	if !EventMatchesKind(SubKindGitHubCI, "check_run") {
		t.Fatal("ci match")
	}
}

func TestFormatHookNotice(t *testing.T) {
	s := FormatHookNotice(SubKindGitHubCI, "a/b", "Check ok")
	for _, part := range []string{"CI", "a/b", "Check ok", "GitHub"} {
		if !strings.Contains(s, part) {
			t.Fatalf("missing %q in %q", part, s)
		}
	}
}

func TestHookPathFor(t *testing.T) {
	if got := HookPathFor("PSUB-1"); got != "/v1/hooks/github/PSUB-1" {
		t.Fatalf("%q", got)
	}
}
