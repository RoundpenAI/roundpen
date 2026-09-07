package settings_test

import (
	"encoding/json"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestDecodeAndMarshalDefaultModel(t *testing.T) {
	fallback := settings.AppSettings{
		DefaultImage:           "host",
		DefaultTtlSeconds:      60,
		PreviewTokenTtlSeconds: 60,
		LlmgwDefaultModel:      "from-fallback",
	}
	raw := []byte(`{
		"defaultImage":"host",
		"defaultTtlSeconds":60,
		"previewTokenTtlSeconds":60,
		"llmgwEnabled":true,
		"llmgwDefaultModel":"gpt-4o-mini",
		"llmgwLogBodyMaxBytes":-1,
		"llmgwEmbeddingModel":"text-embedding-3-small"
	}`)
	got, err := settings.DecodeAppSettings(raw, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got.LlmgwDefaultModel != "gpt-4o-mini" {
		t.Fatalf("decode = %q", got.LlmgwDefaultModel)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal(err)
	}
	if probe["llmgwDefaultModel"] != "gpt-4o-mini" {
		t.Fatalf("marshal missing field: %s", encoded)
	}

	// Reload as if from DB with empty fallback (process restart, no env default).
	again, err := settings.DecodeAppSettings(encoded, settings.AppSettings{
		DefaultImage: "host", DefaultTtlSeconds: 60, PreviewTokenTtlSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.LlmgwDefaultModel != "gpt-4o-mini" {
		t.Fatalf("reload = %q", again.LlmgwDefaultModel)
	}
}
