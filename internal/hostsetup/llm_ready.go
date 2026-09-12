package hostsetup

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// LLMReady reports whether the control-plane LLM gateway can be used for planning/chat.
func LLMReady(cfg *config.Config) (ready bool, reason string) {
	if cfg == nil || !cfg.LLMGW.Enabled {
		return false, "LLM gateway 未启用"
	}
	hasOpenAI := cfg.LLMGW.OpenAI != nil &&
		strings.TrimSpace(cfg.LLMGW.OpenAI.BaseURL) != "" &&
		strings.TrimSpace(cfg.LLMGW.OpenAI.APIKey) != ""
	hasAnthropic := cfg.LLMGW.Anthropic != nil &&
		strings.TrimSpace(cfg.LLMGW.Anthropic.BaseURL) != "" &&
		strings.TrimSpace(cfg.LLMGW.Anthropic.APIKey) != ""
	if !hasOpenAI && !hasAnthropic {
		return false, "尚未配置上游模型地址与密钥"
	}
	if strings.TrimSpace(cfg.LLMGW.DefaultModel) == "" {
		return false, "尚未设置默认模型"
	}
	return true, ""
}
