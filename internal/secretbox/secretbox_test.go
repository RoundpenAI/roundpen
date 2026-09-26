package secretbox

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	box, err := New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestSealOpenRoundTrip(t *testing.T) {
	box := testBox(t)

	sealed, err := box.Seal("sk-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(sealed) || sealed == "sk-upstream-secret" {
		t.Fatalf("sealed = %q", sealed)
	}
	plain, err := box.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "sk-upstream-secret" {
		t.Fatalf("plain = %q", plain)
	}

	again, err := box.Seal("sk-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	if again == sealed {
		t.Fatal("nonce reuse: identical ciphertexts")
	}

	// Idempotent: sealing a sealed value returns it unchanged.
	resealed, err := box.Seal(sealed)
	if err != nil || resealed != sealed {
		t.Fatalf("reseal = %q, %v", resealed, err)
	}

	// Empty passes through.
	if e, err := box.Seal(""); err != nil || e != "" {
		t.Fatalf("seal empty = %q, %v", e, err)
	}
}

func TestOpenLegacyPlaintext(t *testing.T) {
	box := testBox(t)
	got, err := box.Open("sk-plaintext-from-old-row")
	if err != nil || got != "sk-plaintext-from-old-row" {
		t.Fatalf("legacy open = %q, %v", got, err)
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	box := testBox(t)
	if _, err := box.Open(Prefix + base64.StdEncoding.EncodeToString([]byte("garbage"))); err == nil {
		t.Fatal("expected error for malformed ciphertext")
	}
	sealed, err := box.Seal("secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, Prefix))
	raw[len(raw)-1] ^= 0xff
	tampered := Prefix + base64.StdEncoding.EncodeToString(raw)
	if _, err := box.Open(tampered); err == nil {
		t.Fatal("expected auth failure for tampered ciphertext")
	}
}

func TestLoadOrGeneratePersistsKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "secret.key")

	box1, err := LoadOrGenerate(path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file perm = %o", perm)
	}

	box2, err := LoadOrGenerate(path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box1.Seal("cross-check")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := box2.Open(sealed); err != nil || got != "cross-check" {
		t.Fatalf("reloaded key mismatch: %q, %v", got, err)
	}
}

func TestLoadOrGenerateEnvKeyWins(t *testing.T) {
	envKey := strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
	box, err := LoadOrGenerate("/nonexistent/ignored.key", envKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("env-keyed")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := box.Open(sealed); err != nil || got != "env-keyed" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if _, err := LoadOrGenerate("", "not-a-key", nil); err == nil {
		t.Fatal("expected error for malformed env key")
	}
}
