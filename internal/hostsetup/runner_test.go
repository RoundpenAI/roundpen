package hostsetup

import (
	"context"
	"io"
	"testing"
)

func TestRunnerRejectsUnknownAction(t *testing.T) {
	r := NewRunner(RunnerConfig{RepoRoot: t.TempDir()})
	err := r.Run(context.Background(), "nope", PrivilegeAuto, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}
