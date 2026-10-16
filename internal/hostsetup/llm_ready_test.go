package hostsetup

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestLLMReadyReasonsPointAtSystemAdmin(t *testing.T) {
	st := LLMReady(&config.Config{}, nil)
	if st.Ready {
		t.Fatal("expected not ready when gateway off")
	}
	if !strings.Contains(st.Reason, "系统管理 → LLM 网关") {
		t.Fatalf("reason=%q", st.Reason)
	}
	if strings.Contains(st.Reason, "设置 → LLM 网关") {
		t.Fatalf("must not point at personal settings: %q", st.Reason)
	}

	st = LLMReady(&config.Config{LLMGW: config.LLMGWConfig{Enabled: true}}, nil)
	if st.Ready {
		t.Fatal("expected not ready with no upstream")
	}
	if !strings.Contains(st.Reason, "系统管理 → LLM 网关") {
		t.Fatalf("reason=%q", st.Reason)
	}
}
