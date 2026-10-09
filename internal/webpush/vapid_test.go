package webpush

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateVAPIDFromEnv(t *testing.T) {
	v, err := LoadOrGenerateVAPID("", "pub-from-env", "priv-from-env", "mailto:ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if v.PublicKey != "pub-from-env" || v.PrivateKey != "priv-from-env" {
		t.Fatalf("env keys not used: %+v", v)
	}
	if v.Subject != "mailto:ops@example.com" {
		t.Fatalf("subject = %q", v.Subject)
	}
}

func TestLoadOrGenerateVAPIDRequiresBothEnvKeys(t *testing.T) {
	if _, err := LoadOrGenerateVAPID("", "only-pub", "", ""); err == nil {
		t.Fatal("expected error when only public key is set")
	}
}

func TestLoadOrGenerateVAPIDPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "webpush-vapid.json")
	first, err := LoadOrGenerateVAPID(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey == "" || first.PrivateKey == "" {
		t.Fatalf("generated empty keys: %+v", first)
	}
	if first.Subject != "mailto:admin@localhost" {
		t.Fatalf("default subject = %q", first.Subject)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk VAPID
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.PublicKey != first.PublicKey || disk.PrivateKey != first.PrivateKey {
		t.Fatalf("disk mismatch: %+v vs %+v", disk, first)
	}

	second, err := LoadOrGenerateVAPID(path, "", "", "mailto:other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if second.PublicKey != first.PublicKey || second.PrivateKey != first.PrivateKey {
		t.Fatalf("reload regenerated keys: %+v", second)
	}
}

func TestSessionURLAndTruncateTitle(t *testing.T) {
	if got := SessionURL("a1", "s1"); got != "/a/a1/s/s1" {
		t.Fatalf("SessionURL = %q", got)
	}
	if got := TruncateTitle("short", 48); got != "short" {
		t.Fatalf("TruncateTitle short = %q", got)
	}
	long := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOP"
	got := TruncateTitle(long, 10)
	if len([]rune(got)) > 10 && len(got) > 10 {
		// ASCII: max-1 + ellipsis
	}
	if got != "abcdefghi…" {
		t.Fatalf("TruncateTitle long = %q", got)
	}
}
