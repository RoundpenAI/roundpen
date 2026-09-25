package userenv

import (
	"context"
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type fakeIdentities struct {
	creds []gitcred.Cred
	err   error
}

func (f *fakeIdentities) CredsForUser(context.Context, string) ([]gitcred.Cred, error) {
	return f.creds, f.err
}

func TestGitCredentialsFallsBackToStoredTokens(t *testing.T) {
	ctx := context.Background()
	svc := &Service{IdentityTokens: &fakeIdentities{creds: []gitcred.Cred{
		gitcred.TokenCred("gitea", "git.eaxi.com", "oauth-token"),
	}}}

	creds, err := svc.gitCredentials(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Token != "oauth-token" {
		t.Fatalf("creds = %+v", creds)
	}
}

func TestGitCredentialsIgnoresIdentityErrors(t *testing.T) {
	ctx := context.Background()
	svc := &Service{IdentityTokens: &fakeIdentities{err: errors.New("boom")}}

	creds, err := svc.gitCredentials(ctx, "alice")
	if err != nil {
		t.Fatalf("an identity lookup failure must not break injection: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("creds = %+v", creds)
	}
}

func TestReinjectGitWritesIdentityCredentialsToRunningSandbox(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	sb, err := boxes.Create(ctx, sandbox.CreateRequest{ID: "agent-alice", Name: "agent-alice"})
	if err != nil {
		t.Fatal(err)
	}
	slots := &memSlots{}
	if err := slots.Upsert(ctx, "alice", SlotAgent, sb.ID, "code-agent"); err != nil {
		t.Fatal(err)
	}
	idents := &fakeIdentities{creds: []gitcred.Cred{
		gitcred.TokenCred("gitea", "git.eaxi.com", "oauth-token"),
	}}
	svc := &Service{Store: slots, Sandboxes: boxes, IdentityTokens: idents}

	svc.ReinjectGit(ctx, "alice")

	if len(boxes.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(boxes.execs))
	}
	want := gitcred.InstallScript(idents.creds)
	cmd := boxes.execs[0].Cmd
	if len(cmd) == 0 {
		t.Fatal("exec command is empty")
	}
	got := cmd[len(cmd)-1]
	if got != want {
		t.Errorf("script written to the guest does not match the merged credentials:\n%q\n%q", got, want)
	}
}

func TestReinjectGitSkipsWithoutRunningSandbox(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes, IdentityTokens: &fakeIdentities{}}

	svc.ReinjectGit(ctx, "alice")

	if len(boxes.execs) != 0 {
		t.Fatalf("execs = %d, want none without a slot mapping", len(boxes.execs))
	}
}
