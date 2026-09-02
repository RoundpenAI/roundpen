package browser

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestHubAttachDockerWithoutDialer(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	h.SetConfig(&config.Config{
		Backend: "kern",
		CDP:     config.CDPConfig{Provider: config.CDPProviderDocker, Port: 9222},
	})
	_, err := h.Ensure(t.Context(), "sb-1")
	if err == nil {
		t.Fatal("expected docker cdp to fail without a dialer")
	}
}
