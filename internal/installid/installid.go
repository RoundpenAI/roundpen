// Package installid persists a per-install identity used to authenticate
// Roundpen's built-in FREE LLM cloud entry without embedding a product private key.
//
// Token format (Bearer / vkey):
//
//	rp1.<install_id>.<exp_unix>.<base64url(sig)>
//	sig = Ed25519-Sign(private, "rp1|" + install_id + "|" + exp)
package installid

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	tokenPrefix = "rp1"
	signSep     = "|"
)

// hostIDPaths is the preferred order for a stable device id on fnOS / Linux.
// Overridden in tests.
var hostIDPaths = []string{
	"/etc/device_id",           // fnOS hardware / install device id
	"/usr/trim/etc/machine_id", // trim machine_id (often from DMI)
	"/etc/machine-id",          // systemd
}

// Identity is one Roundpen installation's durable credentials.
type Identity struct {
	InstallID string
	PublicKey ed25519.PublicKey

	priv ed25519.PrivateKey
	ttl  time.Duration

	mu     sync.Mutex
	cached string
	expAt  time.Time
}

// Dir under ROUNDPEN_DATA_ROOT holding install_id + key material.
func Dir(dataRoot string) string {
	return filepath.Join(dataRoot, "install-identity")
}

// LoadOrGenerate ensures an install identity exists under dir.
// ttl controls MintToken lifetime (default 1h when <= 0).
//
// On first create, InstallID prefers a host device id (/etc/device_id, …);
// otherwise a random UUID. Existing on-disk identity is never rewritten.
func LoadOrGenerate(dir string, ttl time.Duration, logger *slog.Logger) (*Identity, error) {
	if dir == "" {
		return nil, errors.New("installid: directory required")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	idPath := filepath.Join(dir, "install_id")
	privPath := filepath.Join(dir, "ed25519")
	pubPath := filepath.Join(dir, "ed25519.pub")

	idRaw, idErr := os.ReadFile(idPath)
	privRaw, privErr := os.ReadFile(privPath)

	switch {
	case idErr == nil && privErr == nil:
		id := strings.TrimSpace(string(idRaw))
		if id == "" {
			return nil, fmt.Errorf("installid: empty %s", idPath)
		}
		privHex := strings.TrimSpace(string(privRaw))
		privBytes, err := hex.DecodeString(privHex)
		if err != nil || len(privBytes) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("installid: invalid private key in %s", privPath)
		}
		priv := ed25519.PrivateKey(privBytes)
		pub := priv.Public().(ed25519.PublicKey)
		_ = os.WriteFile(pubPath, []byte(hex.EncodeToString(pub)+"\n"), 0o644)
		return &Identity{InstallID: id, PublicKey: pub, priv: priv, ttl: ttl}, nil
	case errors.Is(idErr, os.ErrNotExist) && errors.Is(privErr, os.ErrNotExist):
		// generate below
	case idErr != nil && !errors.Is(idErr, os.ErrNotExist):
		return nil, idErr
	case privErr != nil && !errors.Is(privErr, os.ErrNotExist):
		return nil, privErr
	default:
		return nil, fmt.Errorf("installid: incomplete identity under %s (need both install_id and ed25519)", dir)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id, idSource := chooseInstallID()
	if err := writeExclusive(idPath, []byte(id+"\n"), 0o644); err != nil {
		return nil, err
	}
	if err := writeExclusive(privPath, []byte(hex.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return nil, err
	}
	if err := writeExclusive(pubPath, []byte(hex.EncodeToString(pub)+"\n"), 0o644); err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Info("generated install identity", "install_id", id, "id_source", idSource, "dir", dir)
	}
	return &Identity{InstallID: id, PublicKey: pub, priv: priv, ttl: ttl}, nil
}

// chooseInstallID picks a stable host device id when available, else a UUID.
func chooseInstallID() (id, source string) {
	for _, path := range hostIDPaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if cleaned := sanitizeInstallID(string(raw)); cleaned != "" {
			return cleaned, path
		}
	}
	return uuid.NewString(), "uuid"
}

// sanitizeInstallID normalizes a host device id for use inside rp1 tokens
// (dot-separated). Rejects empty / placeholder values.
func sanitizeInstallID(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "\"'")
	if s == "" {
		return ""
	}
	// Take first line only (some files may have comments).
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// rp1 tokens split on '.'; host ids must not contain dots.
	if strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ".", "")
	}
	if s == "" || isPlaceholderHostID(s) {
		return ""
	}
	// Keep URL/token-safe charset; drop anything else.
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) < 8 || isPlaceholderHostID(out) {
		return ""
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func isPlaceholderHostID(s string) bool {
	// Seen on boards without DMI: long zero prefix + "trim" hex, etc.
	trimmed := strings.TrimLeft(s, "0")
	return trimmed == "" || trimmed == "5452494d" // "TRIM" in hex
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// PublicKeyHex returns the hex-encoded public key (for TOFU registration).
func (id *Identity) PublicKeyHex() string {
	return hex.EncodeToString(id.PublicKey)
}

// MintToken returns a short-lived rp1 token, reusing a cached one until near expiry.
func (id *Identity) MintToken() (string, error) {
	if id == nil || len(id.priv) == 0 {
		return "", errors.New("installid: nil identity")
	}
	now := time.Now()
	id.mu.Lock()
	defer id.mu.Unlock()
	if id.cached != "" && now.Before(id.expAt.Add(-30*time.Second)) {
		return id.cached, nil
	}
	exp := now.Add(id.ttl).Unix()
	msg := SignMessage(id.InstallID, exp)
	sig := ed25519.Sign(id.priv, []byte(msg))
	token := fmt.Sprintf("%s.%s.%d.%s",
		tokenPrefix,
		id.InstallID,
		exp,
		base64.RawURLEncoding.EncodeToString(sig),
	)
	id.cached = token
	id.expAt = time.Unix(exp, 0)
	return token, nil
}

// SignMessage is the exact byte string signed for an rp1 token.
func SignMessage(installID string, expUnix int64) string {
	return tokenPrefix + signSep + installID + signSep + strconv.FormatInt(expUnix, 10)
}

// Parsed is a decoded rp1 token.
type Parsed struct {
	InstallID string
	ExpUnix   int64
	Sig       []byte
}

// ParseToken splits an rp1 token without verifying the signature.
func ParseToken(token string) (Parsed, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != tokenPrefix {
		return Parsed{}, errors.New("installid: malformed token")
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Parsed{}, errors.New("installid: bad exp")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return Parsed{}, errors.New("installid: bad sig encoding")
	}
	return Parsed{InstallID: parts[1], ExpUnix: exp, Sig: sig}, nil
}

// VerifyToken checks signature and expiry against pub.
func VerifyToken(token string, pub ed25519.PublicKey, now time.Time) (Parsed, error) {
	p, err := ParseToken(token)
	if err != nil {
		return Parsed{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.Unix() > p.ExpUnix {
		return Parsed{}, errors.New("installid: token expired")
	}
	msg := SignMessage(p.InstallID, p.ExpUnix)
	if !ed25519.Verify(pub, []byte(msg), p.Sig) {
		return Parsed{}, errors.New("installid: bad signature")
	}
	return p, nil
}

// SetHostIDPathsForTest overrides host device-id lookup paths (tests only).
// Pass nil to restore defaults.
func SetHostIDPathsForTest(paths []string) {
	if paths == nil {
		hostIDPaths = []string{
			"/etc/device_id",
			"/usr/trim/etc/machine_id",
			"/etc/machine-id",
		}
		return
	}
	hostIDPaths = append([]string{}, paths...)
}

// SanitizeInstallIDForTest exports sanitizeInstallID for unit tests.
func SanitizeInstallIDForTest(raw string) string { return sanitizeInstallID(raw) }
