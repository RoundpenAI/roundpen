package llmgw

import "testing"

func TestEmbeddingAliasMapsToDefaultModel(t *testing.T) {
	m, err := NewModelMatcher(map[string]string{
		EmbeddingModelAlias: DefaultEmbeddingModel,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m.Map(EmbeddingModelAlias)
	if !ok || got != DefaultEmbeddingModel {
		t.Fatalf("Map()=%q,%v want %s,true", got, ok, DefaultEmbeddingModel)
	}
	if EmbeddingDimensions != 1024 {
		t.Fatalf("EmbeddingDimensions=%d want 1024", EmbeddingDimensions)
	}
	if InternalVirtualKey == "" || EmbeddingModelAlias == "" {
		t.Fatal("internal key/alias empty")
	}
}
