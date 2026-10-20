package httpx_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func TestConsolePath(t *testing.T) {
	httpx.SetUIBase("")
	if got := httpx.ConsolePath("/settings/apps"); got != "/settings/apps" {
		t.Fatalf("empty base: %q", got)
	}

	httpx.SetUIBase("/app/roundpen")
	t.Cleanup(func() { httpx.SetUIBase("") })

	cases := map[string]string{
		"/settings/apps":                 "/app/roundpen/settings/apps",
		"/login":                         "/app/roundpen/login",
		"/app/roundpen/settings/apps":    "/app/roundpen/settings/apps",
		"/app/roundpen/settings/apps?x=1": "/app/roundpen/settings/apps?x=1",
		"":                               "/app/roundpen/",
	}
	for in, want := range cases {
		if got := httpx.ConsolePath(in); got != want {
			t.Errorf("ConsolePath(%q)=%q, want %q", in, got, want)
		}
	}
}
