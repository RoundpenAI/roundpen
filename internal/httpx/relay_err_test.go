package httpx_test

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func TestFormatRelayErrorUnknownKeepsDetail(t *testing.T) {
	got := httpx.FormatRelayError("/github/start", 400, []byte(`{"error":"weird upstream"}`))
	if !strings.Contains(got, "HTTP 400") || !strings.Contains(got, "weird upstream") {
		t.Fatalf("unknown = %q", got)
	}
}

func TestRelayErrSuffix(t *testing.T) {
	if got := httpx.RelayErrSuffix(nil); got != "" {
		t.Fatalf("empty = %q", got)
	}
	got := httpx.RelayErrSuffix([]byte(`{"error":"plain boom"}`))
	if got != ": plain boom" {
		t.Fatalf("json = %q", got)
	}
}
