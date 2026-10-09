package installid_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/installid"
)

func TestLoadOrGenerateAndMint(t *testing.T) {
	dir := t.TempDir()
	id1, err := installid.LoadOrGenerate(dir, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id1.InstallID == "" || len(id1.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("bad identity: %+v", id1)
	}
	tok, err := id1.MintToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installid.VerifyToken(tok, id1.PublicKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	tok2, err := id1.MintToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != tok2 {
		t.Fatal("expected cached token")
	}

	id2, err := installid.LoadOrGenerate(dir, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id2.InstallID != id1.InstallID {
		t.Fatalf("reloaded id %q != %q", id2.InstallID, id1.InstallID)
	}
	if hex.EncodeToString(id2.PublicKey) != hex.EncodeToString(id1.PublicKey) {
		t.Fatal("public key mismatch on reload")
	}
}

func TestVerifyRejectsBadSigAndExpired(t *testing.T) {
	dir := t.TempDir()
	id, err := installid.LoadOrGenerate(dir, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := id.MintToken()
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if _, err := installid.VerifyToken(tok, otherPub, time.Now()); err == nil {
		t.Fatal("expected bad signature")
	}
	if _, err := installid.VerifyToken(tok, id.PublicKey, time.Now().Add(2*time.Hour)); err == nil {
		t.Fatal("expected expired")
	}
}

func TestIncompleteIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install_id"), []byte("only-id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installid.LoadOrGenerate(dir, 0, nil); err == nil {
		t.Fatal("expected incomplete identity error")
	}
}

func TestPreferDeviceID(t *testing.T) {
	host := t.TempDir()
	devicePath := filepath.Join(host, "device_id")
	if err := os.WriteFile(devicePath, []byte("6599dcb04b5646a8b43c04e17d7e7f27b3dfa893\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installid.SetHostIDPathsForTest([]string{devicePath})
	t.Cleanup(func() { installid.SetHostIDPathsForTest(nil) })

	dir := t.TempDir()
	id, err := installid.LoadOrGenerate(dir, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id.InstallID != "6599dcb04b5646a8b43c04e17d7e7f27b3dfa893" {
		t.Fatalf("install_id=%q", id.InstallID)
	}
}

func TestSanitizeRejectsPlaceholder(t *testing.T) {
	if got := installid.SanitizeInstallIDForTest("000000000000000000000000000000005452494d"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := installid.SanitizeInstallIDForTest("AB.CD-ef_12"); got != "abcd-ef_12" {
		t.Fatalf("got %q", got)
	}
}

// TestVector_KnownKey locks a fixed seed → pubkey → token for cloud alignment.
// Keep docs/free-llm-token.md in sync when this fails.
func TestVector_KnownKey(t *testing.T) {
	seed, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	installID := "11111111-2222-3333-4444-555555555555"
	exp := int64(1893456000) // 2030-01-01T00:00:00Z
	msg := installid.SignMessage(installID, exp)
	sig := ed25519.Sign(priv, []byte(msg))
	token := "rp1." + installID + ".1893456000." + base64.RawURLEncoding.EncodeToString(sig)

	const (
		expectPub = "207a067892821e25d770f1fba0c47c11ff4b813e54162ece9eb839e076231ab6"
		expectMsg = "rp1|11111111-2222-3333-4444-555555555555|1893456000"
	)
	if hex.EncodeToString(pub) != expectPub {
		t.Fatalf("public_key_hex=%s want %s", hex.EncodeToString(pub), expectPub)
	}
	if msg != expectMsg {
		t.Fatalf("message=%q want %q", msg, expectMsg)
	}
	if _, err := installid.VerifyToken(token, pub, time.Unix(exp-1, 0)); err != nil {
		t.Fatal(err)
	}
	const lockedToken = "rp1.11111111-2222-3333-4444-555555555555.1893456000.xN_w9bG-00T3TQX39B40w1DMmLkn0WEid4wGY5tSeCYwhd0wQR_6YUniaX1qkQEBPL-YK6-jrRKFroqxLzS8CA"
	if token != lockedToken {
		t.Fatalf("token=%s want %s (update docs/free-llm-token.md)", token, lockedToken)
	}
}
