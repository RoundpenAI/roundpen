package agentapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func TestIsDeadlineExceeded(t *testing.T) {
	if !isDeadlineExceeded(context.DeadlineExceeded) {
		t.Fatal("plain DeadlineExceeded")
	}
	wrapped := acp.NewInternalError(map[string]any{"error": context.DeadlineExceeded.Error()})
	if !isDeadlineExceeded(wrapped) {
		t.Fatalf("ACP-wrapped deadline: %v", wrapped)
	}
	if isDeadlineExceeded(errors.New("other")) {
		t.Fatal("non-deadline should be false")
	}
}

func TestTurnErrorMessage(t *testing.T) {
	msg := turnErrorMessage(context.DeadlineExceeded)
	if !strings.Contains(msg, "超时") {
		t.Fatalf("want friendly timeout text, got %q", msg)
	}
	if !strings.Contains(msg, "2 小时") {
		t.Fatalf("want duration in message, got %q", msg)
	}
	other := turnErrorMessage(errors.New("boom"))
	if other != "boom" {
		t.Fatalf("passthrough: %q", other)
	}
}

func TestFormatTurnTimeout(t *testing.T) {
	if got := formatTurnTimeout(2 * time.Hour); got != "2 小时" {
		t.Fatalf("got %q", got)
	}
	if got := formatTurnTimeout(10 * time.Minute); got != "10 分钟" {
		t.Fatalf("got %q", got)
	}
}
