package llmgw

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/search"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// Built-in platform ids. VendorCustom keeps the hand-filled protocol and base URL.
const (
	VendorCustom     = "custom"
	VendorDeepSeek   = "deepseek"
	VendorKimi       = "kimi"
	VendorGLM        = "glm"
	VendorOpenRouter = "openrouter"
)

// VendorModel is one entry in a built-in platform's model dropdown.
type VendorModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Vendor is a platform the gateway knows how to dial: base URL, protocol,
// model list, and whether its native web search can back the WebSearch tool.
type Vendor struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Protocol string        `json:"protocol"`
	BaseURL  string        `json:"baseUrl"`
	Models   []VendorModel `json:"models"`
	// Search is true when the platform's own web search can be used directly.
	Search bool `json:"search"`
	// SearchBase is the host the search adapter calls. Empty means no search.
	SearchBase string `json:"-"`
	// listed is set only by LookupVendor for an id that is in the catalog.
	listed bool
}

// Preset reports whether this vendor fills protocol and base URL itself.
func (v Vendor) Preset() bool {
	return v.listed && v.ID != VendorCustom
}

// AllowsModel reports whether model is in the dropdown. OpenRouter also
// accepts any provider/model id so a curated list is not a hard lock.
func (v Vendor) AllowsModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, m := range v.Models {
		if m.ID == model {
			return true
		}
	}
	if v.ID == VendorOpenRouter {
		return openRouterModelID(model)
	}
	return false
}

func openRouterModelID(model string) bool {
	if len(model) > 128 || !strings.Contains(model, "/") {
		return false
	}
	for _, r := range model {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '@' || r == '+':
		default:
			return false
		}
	}
	return true
}

// Vendors returns the built-in platforms in dropdown order, custom last.
func Vendors() []Vendor {
	return []Vendor{
		{
			ID:       VendorDeepSeek,
			Name:     "DeepSeek",
			Protocol: ProtocolOpenAI,
			BaseURL:  "https://api.deepseek.com",
			Models: []VendorModel{
				{ID: "deepseek-flash", Label: "DeepSeek Flash"},
				{ID: "deepseek-v4-pro", Label: "DeepSeek V4 Pro"},
			},
			Search:     true,
			SearchBase: "https://api.deepseek.com/anthropic/v1",
		},
		{
			ID:       VendorKimi,
			Name:     "Kimi",
			Protocol: ProtocolOpenAI,
			BaseURL:  "https://api.moonshot.cn/v1",
			Models: []VendorModel{
				{ID: "kimi-k3", Label: "Kimi K3"},
				{ID: "kimi-k2.6", Label: "Kimi K2.6"},
				{ID: "kimi-k2.7-code", Label: "Kimi K2.7 Code"},
				{ID: "kimi-k2.7-code-highspeed", Label: "Kimi K2.7 Code HighSpeed"},
			},
			Search:     true,
			SearchBase: "https://api.moonshot.cn/v1",
		},
		{
			ID:       VendorGLM,
			Name:     "GLM",
			Protocol: ProtocolOpenAI,
			BaseURL:  "https://open.bigmodel.cn/api/paas/v4",
			Models: []VendorModel{
				{ID: "glm-5.3", Label: "GLM-5.3"},
				{ID: "glm-5.3-flash", Label: "GLM-5.3 Flash"},
				{ID: "glm-5.2", Label: "GLM-5.2"},
				{ID: "glm-5.1", Label: "GLM-5.1"},
				{ID: "glm-4.7", Label: "GLM-4.7"},
				{ID: "glm-4.7-flash", Label: "GLM-4.7 Flash"},
				{ID: "glm-4.6", Label: "GLM-4.6"},
			},
			Search:     true,
			SearchBase: "https://open.bigmodel.cn/api/paas/v4",
		},
		{
			ID:       VendorOpenRouter,
			Name:     "OpenRouter",
			Protocol: ProtocolOpenAI,
			BaseURL:  "https://openrouter.ai/api/v1",
			Models: []VendorModel{
				{ID: "openrouter/auto", Label: "Auto"},
				{ID: "openai/gpt-5.2", Label: "GPT-5.2"},
				{ID: "openai/gpt-5-mini", Label: "GPT-5 mini"},
				{ID: "anthropic/claude-sonnet-4.5", Label: "Claude Sonnet 4.5"},
				{ID: "anthropic/claude-opus-4.5", Label: "Claude Opus 4.5"},
				{ID: "google/gemini-2.5-pro", Label: "Gemini 2.5 Pro"},
				{ID: "google/gemini-2.5-flash", Label: "Gemini 2.5 Flash"},
				{ID: "deepseek/deepseek-v4-pro", Label: "DeepSeek V4 Pro"},
				{ID: "x-ai/grok-4", Label: "Grok 4"},
				{ID: "qwen/qwen3-235b-a22b", Label: "Qwen3 235B"},
			},
			Search:     true,
			SearchBase: "https://openrouter.ai/api/v1",
		},
		{
			ID:       VendorCustom,
			Name:     "Custom",
			Protocol: ProtocolOpenAI,
			Models:   []VendorModel{},
		},
	}
}

// VendorIDs is the enum stored on an LLM item.
func VendorIDs() []string {
	all := Vendors()
	out := make([]string, len(all))
	for i, v := range all {
		out[i] = v.ID
	}
	return out
}

// LookupVendor returns the catalog entry for id. An empty or unknown id
// yields the custom vendor with Preset false; the bool is true only for a
// known id including custom.
func LookupVendor(id string) Vendor {
	id = strings.TrimSpace(id)
	if id == "" {
		id = VendorCustom
	}
	for _, v := range Vendors() {
		if v.ID == id {
			v.listed = true
			return v
		}
	}
	return Vendor{}
}

// VendorSearch is the native web-search backend of the LLM provider a slot
// resolves to. A zero Config means that provider has no built-in search (or
// no key), and the caller keeps the dedicated search item.
func VendorSearch(snap *settingitems.Snapshot, user string) search.Config {
	if snap == nil {
		return search.Config{}
	}
	target, ok := Resolve(snap, settingitems.SlotLLMSysAgent, user)
	if !ok {
		target, ok = Resolve(snap, settingitems.SlotLLMDefault, user)
	}
	if !ok {
		return search.Config{}
	}
	it, ok := snap.Item(settingitems.KindLLM, target.ItemID)
	if !ok {
		return search.Config{}
	}
	if it.String("authMode") == AuthModeInstallIdentity {
		return search.Config{}
	}
	vendor := LookupVendor(it.String("vendor"))
	if !vendor.Search || it.Secret("apiKey") == "" {
		return search.Config{}
	}
	model := target.Model
	if model == "" {
		model = it.String("defaultModel")
	}
	return search.Config{
		APIKey:   it.Secret("apiKey"),
		ProxyURL: search.ProxyURL(snap, it.String("proxyId")),
		Vendor:   vendor.ID,
		BaseURL:  vendor.SearchBase,
		Model:    model,
	}
}
