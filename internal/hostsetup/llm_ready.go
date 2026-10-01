package hostsetup

import (
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// LLMStatus is the cold-start readiness payload for GET /v1/setup/llm-ready
// and POST /v1/setup/llm-free.
type LLMStatus struct {
	Ready         bool   `json:"ready"`
	Mode          string `json:"mode"` // free | byok | none
	Reason        string `json:"reason,omitempty"`
	FreeAvailable bool   `json:"freeAvailable"`
}

// LLMReady reports whether the control-plane LLM gateway can be used for planning/chat.
func LLMReady(cfg *config.Config, snap *settingitems.Snapshot) LLMStatus {
	free := llmgw.LoadFreeEnv()
	st := LLMStatus{FreeAvailable: free.Available(), Mode: "none"}
	if cfg == nil || !cfg.LLMGW.Enabled {
		st.Reason = "LLM 网关未启用。可一键启用免费试用，或到「设置 → LLM 网关」配置自己的 API Key。"
		return st
	}
	mode := llmgw.ModeOf(true, snap)
	st.Mode = mode
	switch mode {
	case "free", "byok":
		st.Ready = true
		return st
	default:
		st.Reason = "尚未配置上游模型。可一键启用免费试用，或到「设置 → LLM 网关」添加提供商。"
		return st
	}
}
