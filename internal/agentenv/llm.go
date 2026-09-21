package agentenv

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// LLMEnv describes the llmgw endpoints and model pins a sandbox receives.
type LLMEnv struct {
	// AnthropicProvider and OpenAIProvider are provider item ids; an empty id
	// omits that endpoint. Sandboxes typically get both so Claude Code and
	// OpenAI-compatible CLIs each have a relay URL.
	AnthropicProvider string
	OpenAIProvider    string
	// Model pins Claude Code's model selection so it does not fall back to a
	// built-in model the upstream does not serve.
	Model string
	// ModeModels pins individual mode slots (plan / vision / coding); an entry
	// only applies when its slot uses the same provider as Model.
	ModeModels map[string]string
	// Extra carries provider descriptors (ROUNDPEN_LLM_*) consumers can read.
	Extra map[string]string
}

// Empty reports whether no relay endpoint is configured.
func (e LLMEnv) Empty() bool { return e.AnthropicProvider == "" && e.OpenAIProvider == "" }

// ModeEnvKeys maps llm mode slots to the client variables they pin.
var ModeEnvKeys = map[string][]string{
	settingitems.SlotLLMPlan:   {"ANTHROPIC_DEFAULT_OPUS_MODEL"},
	settingitems.SlotLLMVision: {"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_SMALL_FAST_MODEL"},
	settingitems.SlotLLMCoding: {"CLAUDE_CODE_SUBAGENT_MODEL"},
}

// modelPinKeys are the variables the agent slot pins to its model.
var modelPinKeys = []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL"}

// ApplyLLMEnv writes the relay endpoints, credentials and model pins into env.
// Existing values win: an operator-set variable is never overwritten.
func ApplyLLMEnv(env map[string]string, base, key string, cfg LLMEnv) {
	if env == nil || cfg.Empty() {
		return
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if id := strings.TrimSpace(cfg.AnthropicProvider); id != "" {
		env["ANTHROPIC_BASE_URL"] = base + "/llmgw/" + id
		env["ANTHROPIC_API_KEY"] = key
		env["ANTHROPIC_AUTH_TOKEN"] = key
	}
	if id := strings.TrimSpace(cfg.OpenAIProvider); id != "" {
		env["OPENAI_BASE_URL"] = base + "/llmgw/" + id
		env["OPENAI_API_KEY"] = key
	}
	pinAll(env, modelPinKeys, cfg.Model)
	for slot, keys := range ModeEnvKeys {
		pinAll(env, keys, cfg.ModeModels[slot])
	}
	for k, v := range cfg.Extra {
		if _, ok := env[k]; !ok {
			env[k] = v
		}
	}
}

func pinAll(env map[string]string, keys []string, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	for _, k := range keys {
		if strings.TrimSpace(env[k]) == "" {
			env[k] = model
		}
	}
}
