package expertrun_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/expertrun"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type fakeSlot struct {
	id string
}

func (f fakeSlot) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: f.id, Status: sandbox.StatusRunning}, nil
}

type fakeDriver struct {
	reply string
	stop  string
	err   error
	calls int
}

func (f *fakeDriver) RunOnce(context.Context, string, string, []string, string, authz.Actor) (string, string, error) {
	f.calls++
	return f.reply, f.stop, f.err
}

type fakeSandbox struct {
	sandbox.Manager
	stdout string
}

func (f *fakeSandbox) AttachExec(context.Context, string, sandbox.AttachExecOpts, io.Reader, io.Writer, io.Writer) error {
	return nil
}
func (f *fakeSandbox) Exec(context.Context, string, sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	return &sandbox.ExecResult{Stdout: []byte(f.stdout)}, nil
}

func TestRunner_RunSuccess(t *testing.T) {
	drv := &fakeDriver{
		reply: "```json\n{\"status\":\"succeeded\",\"summary\":\"fixed\",\"changedPaths\":[\"x.go\"],\"prIntent\":{\"action\":\"open_pr\"}}\n```",
		stop:  "end_turn",
	}
	r := expertrun.New(nil, fakeSlot{id: "sb-1"}, &fakeSandbox{stdout: " M x.go\n"})
	r.Driver = drv
	res, err := r.Run(context.Background(), expertrun.Request{
		UserID:   "alice",
		Actor:    authz.Actor{Username: "alice"},
		ExpertID: "claude",
		Command:  "claude-agent-acp",
		Goal:     "fix x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "succeeded" || res.Summary != "fixed" || len(res.ChangedPaths) != 1 {
		t.Fatalf("%+v", res)
	}
	if drv.calls != 1 {
		t.Fatalf("calls=%d", drv.calls)
	}
}

func TestRunner_RetryTransient(t *testing.T) {
	drv := &flakyDriver{failOnce: true, reply: "```json\n{\"status\":\"succeeded\",\"summary\":\"ok\"}\n```"}
	r := expertrun.New(nil, fakeSlot{id: "sb"}, &fakeSandbox{})
	r.Driver = drv
	res, err := r.Run(context.Background(), expertrun.Request{
		UserID: "u", Actor: authz.Actor{Username: "u"}, ExpertID: "claude", Command: "claude-agent-acp", Goal: "g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Retried || res.Status != "succeeded" {
		t.Fatalf("%+v", res)
	}
}

type flakyDriver struct {
	failOnce bool
	calls    int
	reply    string
}

func (f *flakyDriver) RunOnce(context.Context, string, string, []string, string, authz.Actor) (string, string, error) {
	f.calls++
	if f.failOnce && f.calls == 1 {
		return "", "", errors.New("connection reset by peer")
	}
	return f.reply, "end_turn", nil
}

func TestRunner_Busy(t *testing.T) {
	block := make(chan struct{})
	drv := &blockingDriver{block: block}
	r := expertrun.New(nil, fakeSlot{id: "sb"}, &fakeSandbox{})
	r.Driver = drv
	go func() {
		_, _ = r.Run(context.Background(), expertrun.Request{
			UserID: "u", Actor: authz.Actor{Username: "u"}, ExpertID: "claude", Command: "c", Goal: "g",
		})
	}()
	// Wait until busy
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.Busy("u") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !r.Busy("u") {
		t.Fatal("expected busy")
	}
	_, err := r.Run(context.Background(), expertrun.Request{
		UserID: "u", Actor: authz.Actor{Username: "u"}, ExpertID: "claude", Command: "c", Goal: "g",
	})
	if err == nil {
		t.Fatal("expected busy error")
	}
	close(block)
}

type blockingDriver struct {
	block chan struct{}
}

func (b *blockingDriver) RunOnce(ctx context.Context, _ string, _ string, _ []string, _ string, _ authz.Actor) (string, string, error) {
	select {
	case <-b.block:
		return "```json\n{\"status\":\"succeeded\",\"summary\":\"x\"}\n```", "end_turn", nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func TestRunner_NeedsAuth(t *testing.T) {
	r := expertrun.New(nil, fakeSlot{id: "sb"}, &fakeSandbox{})
	r.Driver = &fakeDriver{err: errors.New("not logged in — run claude auth")}
	res, err := r.Run(context.Background(), expertrun.Request{
		UserID: "u", Actor: authz.Actor{Username: "u"}, ExpertID: "claude", Command: "c", Goal: "g",
	})
	if !errors.Is(err, expertrun.ErrNeedsAuth) {
		t.Fatalf("err=%v", err)
	}
	if res.Status != expertrun.StatusNeedsAuth {
		t.Fatalf("%+v", res)
	}
}
