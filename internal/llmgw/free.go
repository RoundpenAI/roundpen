package llmgw

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

const (
	// FreeItemID is the built-in FREE cloud provider; cannot be deleted.
	FreeItemID = "roundpen-free"

	// DefaultFreeBaseURL is the fixed Roundpen FREE cloud entry.
	// ROUNDPEN_FREE_LLM_BASE_URL may override for ops; product builds always ship this.
	DefaultFreeBaseURL = "https://api.ttz.ac.cn"

	// AuthModeAPIKey is the default: use the stored upstream apiKey.
	AuthModeAPIKey = "api_key"
	// AuthModeInstallIdentity mints an rp1 install token as the upstream key.
	AuthModeInstallIdentity = "install_identity"
)

// FreeEnv holds FREE LLM cloud-entry settings from the environment.
type FreeEnv struct {
	BaseURL  string
	Model    string
	TokenTTL time.Duration
}

// LoadFreeEnv returns the built-in FREE entry, with optional ROUNDPEN_FREE_LLM_* overrides.
func LoadFreeEnv() FreeEnv {
	ttl := time.Hour
	if raw := strings.TrimSpace(os.Getenv("ROUNDPEN_FREE_LLM_TOKEN_TTL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			ttl = d
		}
	}
	model := strings.TrimSpace(os.Getenv("ROUNDPEN_FREE_LLM_MODEL"))
	if model == "" {
		model = "roundpen-free"
	}
	base := strings.TrimSpace(os.Getenv("ROUNDPEN_FREE_LLM_BASE_URL"))
	if base == "" {
		base = DefaultFreeBaseURL
	}
	return FreeEnv{
		BaseURL:  base,
		Model:    model,
		TokenTTL: ttl,
	}
}

// Available is always true: FREE ships with a fixed cloud entry.
func (e FreeEnv) Available() bool {
	return true
}

// SeedFree upserts the built-in FREE provider and binds default / sysagent slots.
func SeedFree(ctx context.Context, cat *settingitems.Catalog, env FreeEnv) error {
	if cat == nil {
		return fmt.Errorf("llmgw: catalog required")
	}
	if env.BaseURL == "" {
		env.BaseURL = DefaultFreeBaseURL
	}
	if env.Model == "" {
		env.Model = "roundpen-free"
	}
	it := settingitems.Item{
		Kind:        settingitems.KindLLM,
		ID:          FreeItemID,
		Name:        "Roundpen FREE",
		Description: "Built-in free cloud entry (rate-limited). Uses this install's signed identity.",
		Enabled:     true,
		Config: map[string]any{
			"protocol":     ProtocolOpenAI,
			"baseUrl":      env.BaseURL,
			"defaultModel": env.Model,
			"authMode":     AuthModeInstallIdentity,
		},
	}
	if _, err := cat.Save(ctx, it); err != nil {
		return fmt.Errorf("llmgw: seed free item: %w", err)
	}
	for _, slot := range []string{settingitems.SlotLLMDefault, settingitems.SlotLLMSysAgent} {
		if err := cat.SetBinding(ctx, settingitems.Binding{
			Scope:  settingitems.GlobalScope,
			Slot:   slot,
			Kind:   settingitems.KindLLM,
			ItemID: FreeItemID,
			Params: map[string]any{"model": env.Model},
		}); err != nil {
			return fmt.Errorf("llmgw: bind %s: %w", slot, err)
		}
	}
	return nil
}

// ModeOf reports free|byok|none from gateway enablement and slot bindings.
func ModeOf(enabled bool, snap *settingitems.Snapshot) string {
	if !enabled || snap == nil {
		return "none"
	}
	target, ok := Resolve(snap, settingitems.SlotLLMSysAgent, "")
	if !ok {
		target, ok = Resolve(snap, settingitems.SlotLLMDefault, "")
	}
	if !ok {
		return "none"
	}
	if target.ItemID == FreeItemID {
		return "free"
	}
	return "byok"
}
