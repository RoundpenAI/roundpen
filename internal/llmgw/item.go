package llmgw

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// Protocols an LLM item can speak. The protocol picks the relay handler and
// the upstream auth header.
const (
	ProtocolOpenAI    = ProviderOpenAI
	ProtocolAnthropic = ProviderAnthropic
)

// Kind returns the LLM provider kind definition.
func Kind() settingitems.KindDef {
	return settingitems.KindDef{
		Kind: settingitems.KindLLM,
		Name: "LLM provider",
		Fields: []settingitems.Field{
			{
				Key:      "protocol",
				Type:     settingitems.FieldEnum,
				Label:    "Protocol",
				Required: true,
				Options:  []string{ProtocolOpenAI, ProtocolAnthropic},
				Hint:     "Wire format of the upstream. Anthropic keeps x-api-key auth, OpenAI uses a bearer token.",
			},
			{
				Key:      "baseUrl",
				Type:     settingitems.FieldString,
				Label:    "Base URL",
				Required: true,
			},
			{
				Key:      "apiKey",
				Type:     settingitems.FieldSecret,
				Label:    "Upstream API key",
				Required: true,
			},
			{
				Key:     "proxyId",
				Type:    settingitems.FieldItemRef,
				RefKind: settingitems.KindProxy,
				Label:   "Egress proxy",
			},
			{
				Key:   "defaultModel",
				Type:  settingitems.FieldString,
				Label: "Fallback model",
				Hint:  "Used when a request names a model this provider does not know.",
			},
			{
				Key:      "modelMap",
				Type:     settingitems.FieldJSON,
				Label:    "Model aliases",
				Hint:     `Exact alias → upstream model, e.g. {"roundpen-embed": "text-embedding-3-small"}.`,
				Advanced: true,
			},
			{
				Key:      "modelPatterns",
				Type:     settingitems.FieldJSON,
				Label:    "Model patterns",
				Hint:     `Glob/regex rules: [{"pattern": "claude-3-*", "target": "claude-3-5-sonnet"}]`,
				Advanced: true,
			},
		},
		Selectable: []string{},
		Validate:   validateItem,
	}
}

func validateItem(it *settingitems.Item) error {
	switch it.String("protocol") {
	case ProtocolOpenAI, ProtocolAnthropic:
	default:
		return fmt.Errorf("protocol must be %s or %s", ProtocolOpenAI, ProtocolAnthropic)
	}
	if it.String("baseUrl") == "" {
		return fmt.Errorf("baseUrl is required")
	}
	return nil
}

// Slots returns the LLM usage sites. Slots whose call sites have no user
// (classifier, planner, embedding) are global-only.
func Slots() []settingitems.SlotDef {
	modelField := settingitems.Field{Key: "model", Type: settingitems.FieldString, Label: "Model"}
	return []settingitems.SlotDef{
		{
			Key:           settingitems.SlotLLMDefault,
			Kind:          settingitems.KindLLM,
			Name:          "Default provider",
			Description:   "Fallback for every slot that has no selection of its own.",
			BindingFields: []settingitems.Field{modelField},
			UserOverride:  true,
		},
		{
			Key:           settingitems.SlotLLMAgent,
			Kind:          settingitems.KindLLM,
			Name:          "Sandbox agent",
			Description:   "Provider injected into agent sandboxes: ANTHROPIC_MODEL / OPENAI_BASE_URL and friends.",
			BindingFields: []settingitems.Field{modelField},
			UserOverride:  true,
			RebuildsEnv:   true,
			FallbackSlot:  settingitems.SlotLLMDefault,
		},
		{
			Key:           settingitems.SlotLLMSysAgent,
			Kind:          settingitems.KindLLM,
			Name:          "System agent",
			Description:   "Provider of the in-process System Agent (chat and permissions).",
			BindingFields: []settingitems.Field{modelField},
			UserOverride:  true,
			FallbackSlot:  settingitems.SlotLLMDefault,
		},
		{
			Key:           settingitems.SlotLLMClassifier,
			Kind:          settingitems.KindLLM,
			Name:          "Auto-mode classifier",
			Description:   "Model that classifies tool calls behind the chat Auto toggle. Admin-only.",
			BindingFields: []settingitems.Field{modelField},
			FallbackSlot:  settingitems.SlotLLMDefault,
		},
		{
			Key:           settingitems.SlotLLMPlanner,
			Kind:          settingitems.KindLLM,
			Name:          "Setup planner",
			Description:   "Model behind the host-setup wizard. Admin-only.",
			BindingFields: []settingitems.Field{modelField},
			FallbackSlot:  settingitems.SlotLLMDefault,
		},
		{
			Key:           settingitems.SlotLLMPlan,
			Kind:          settingitems.KindLLM,
			Name:          "Plan mode",
			Description:   "Model pinned to ANTHROPIC_DEFAULT_OPUS_MODEL when it uses the same provider as the sandbox agent; otherwise only ROUNDPEN_LLM_PLAN_* is exported.",
			BindingFields: []settingitems.Field{modelField},
			UserOverride:  true,
			FallbackSlot:  settingitems.SlotLLMAgent,
		},
		{
			Key:           settingitems.SlotLLMVision,
			Kind:          settingitems.KindLLM,
			Name:          "Vision mode",
			Description:   "Model pinned to ANTHROPIC_DEFAULT_HAIKU_MODEL / ANTHROPIC_SMALL_FAST_MODEL under the same provider rule.",
			BindingFields: []settingitems.Field{modelField},
			UserOverride:  true,
			FallbackSlot:  settingitems.SlotLLMAgent,
		},
		{
			Key:           settingitems.SlotLLMCoding,
			Kind:          settingitems.KindLLM,
			Name:          "Coding subagent",
			Description:   "Model pinned to CLAUDE_CODE_SUBAGENT_MODEL under the same provider rule.",
			BindingFields: []settingitems.Field{modelField},
			UserOverride:  true,
			FallbackSlot:  settingitems.SlotLLMAgent,
		},
		{
			Key:           settingitems.SlotLLMEmbedding,
			Kind:          settingitems.KindLLM,
			Name:          "Embeddings",
			Description:   "Provider for memory embeddings. Must speak the OpenAI protocol and return 1024 dimensions.",
			Protocols:     []string{ProtocolOpenAI},
			BindingFields: []settingitems.Field{modelField},
			FallbackSlot:  settingitems.SlotLLMDefault,
		},
	}
}

// UpstreamFromItem projects an LLM item onto the relay's upstream view,
// resolving its egress proxy through the same snapshot.
func UpstreamFromItem(snap *settingitems.Snapshot, it settingitems.Item) Upstream {
	up := Upstream{
		Provider:      it.ID,
		Protocol:      it.String("protocol"),
		BaseURL:       trimRightSlash(it.String("baseUrl")),
		APIKey:        it.Secret("apiKey"),
		ModelMap:      it.StringMap("modelMap"),
		DefaultModel:  it.String("defaultModel"),
		Enabled:       it.Enabled,
		ModelPatterns: modelPatterns(it),
	}
	if up.ModelMap == nil {
		up.ModelMap = map[string]string{}
	}
	if id := it.String("proxyId"); id != "" {
		if proxy, ok := snap.Item(settingitems.KindProxy, id); ok {
			up.ProxyURL = proxy.Secret("url")
		}
	}
	return up
}

func modelPatterns(it settingitems.Item) []ModelPattern {
	out := []ModelPattern{}
	for _, raw := range it.List("modelPatterns") {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		pattern, _ := entry["pattern"].(string)
		target, _ := entry["target"].(string)
		if strings.TrimSpace(pattern) == "" || strings.TrimSpace(target) == "" {
			continue
		}
		regex, _ := entry["regex"].(bool)
		out = append(out, ModelPattern{Pattern: pattern, Target: target, Regex: regex})
	}
	return out
}

// UpstreamsFromSnapshot projects every enabled LLM item the relay can serve.
func UpstreamsFromSnapshot(snap *settingitems.Snapshot) []Upstream {
	items := snap.Items(settingitems.KindLLM, false)
	out := make([]Upstream, 0, len(items))
	for _, it := range items {
		out = append(out, UpstreamFromItem(snap, it))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// Target is a resolved LLM slot: the provider item and model a consumer uses.
type Target struct {
	ItemID   string
	Protocol string
	Model    string
}

// RelayPath is the relay prefix a client points its base URL at.
func (t Target) RelayPath() string { return "/llmgw/" + t.ItemID }

// Resolve resolves a slot into a target ("" user = the global default).
func Resolve(snap *settingitems.Snapshot, slot, user string) (Target, bool) {
	if snap == nil {
		return Target{}, false
	}
	res := snap.Resolve(slot, user)
	if !res.OK {
		return Target{}, false
	}
	model := res.Param("model")
	if model == "" {
		model = res.Item.String("defaultModel")
	}
	return Target{ItemID: res.Item.ID, Protocol: res.Item.String("protocol"), Model: model}, true
}

// ProtocolFallback returns the first enabled item speaking protocol, ordered
// by position. Sandbox env uses it for the endpoint a bound slot does not
// cover, so the other protocol stays reachable.
func ProtocolFallback(snap *settingitems.Snapshot, protocol string) (Target, bool) {
	if snap == nil {
		return Target{}, false
	}
	for _, it := range snap.Items(settingitems.KindLLM, false) {
		if it.String("protocol") == protocol {
			return Target{ItemID: it.ID, Protocol: protocol, Model: it.String("defaultModel")}, true
		}
	}
	return Target{}, false
}

// ModelOf returns a slot's pinned model, falling back to the item's default.
func ModelOf(snap *settingitems.Snapshot, slot, user string) string {
	target, ok := Resolve(snap, slot, user)
	if !ok {
		return ""
	}
	return target.Model
}
