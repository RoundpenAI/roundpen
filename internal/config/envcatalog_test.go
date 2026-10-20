package config

import (
	"testing"
	"time"
)

func TestEnvCatalogMasksSecretsAndListsCoreVars(t *testing.T) {
	cfg := &Config{
		DatabaseURL:  "postgres://user:pass@localhost/db",
		HTTPAddr:     ":19001",
		DefaultImage: defaultAgentImage,
		AgentImage:   defaultAgentImage,
		DefaultTTL:   30 * time.Minute,
		CDP:          CDPConfig{Provider: "auto", Port: DefaultCDPPort},
		LLMGW:        LLMGWConfig{Enabled: true, EmbeddingModel: "text-embedding-3-small"},
	}
	rows := EnvCatalog(cfg)
	if len(rows) < 20 {
		t.Fatalf("expected a full catalog, got %d", len(rows))
	}
	byName := map[string]EnvVar{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	db := byName["DATABASE_URL"]
	if !db.Secret || db.Value != maskedSecret {
		t.Fatalf("DATABASE_URL should be masked: %+v", db)
	}
	img := byName["ROUNDPEN_DEFAULT_IMAGE"]
	if img.Value != defaultAgentImage || img.Default != defaultAgentImage {
		t.Fatalf("DEFAULT_IMAGE: %+v", img)
	}
	ttl := byName["ROUNDPEN_DEFAULT_TTL"]
	if ttl.Value != "1800" || ttl.Default != "1800" {
		t.Fatalf("DEFAULT_TTL: %+v", ttl)
	}
	if byName["ROUNDPEN_HTTP_ADDR"].Value != ":19001" {
		t.Fatalf("HTTP_ADDR: %+v", byName["ROUNDPEN_HTTP_ADDR"])
	}
}
