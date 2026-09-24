package settings

import (
	"encoding/json"
	"fmt"
)

// DecodeAppSettings unmarshals stored JSON on top of fallback so newly added
// keys keep env defaults when older rows omit them. Fields that moved to
// setting items are ignored here; Store.LoadLegacy reads them for the one-time
// import of an older document.
func DecodeAppSettings(raw []byte, fallback AppSettings) (AppSettings, error) {
	out := fallback
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return AppSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return out, nil
	}
	if _, ok := keys["llmgwEnabled"]; !ok {
		out.LlmgwEnabled = fallback.LlmgwEnabled
		out.LlmgwPublicURL = fallback.LlmgwPublicURL
		out.LlmgwLogBodyMaxBytes = fallback.LlmgwLogBodyMaxBytes
		out.LlmgwVirtualKeys = fallback.LlmgwVirtualKeys
	}
	if _, ok := keys["autoMode"]; !ok {
		out.AutoMode = fallback.AutoMode
	}
	return out, nil
}
