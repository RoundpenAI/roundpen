// Package secretbox encrypts secrets at rest with AES-256-GCM.
//
// Sealed values carry an "enc:v1:" prefix followed by base64(nonce||ciphertext).
// Values without the prefix are treated as legacy plaintext and returned
// unchanged, so existing database rows keep working and are re-encrypted on
// the next write.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const Prefix = "enc:v1:"

// Box seals and opens secrets with a fixed 256-bit key.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secretbox: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// ParseKey decodes a master key from hex or base64 text (e.g. an env var).
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if raw, err := hex.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	return nil, errors.New("secretbox: key must be 32 bytes, hex or base64 encoded")
}

// LoadOrGenerate returns a Box keyed by envKey (hex/base64) when set,
// otherwise it loads the key file at path, generating and persisting it
// (0600) on first use. The env key takes precedence so deployments can
// manage the master key outside the data directory.
func LoadOrGenerate(path, envKey string, logger *slog.Logger) (*Box, error) {
	if strings.TrimSpace(envKey) != "" {
		key, err := ParseKey(envKey)
		if err != nil {
			return nil, err
		}
		return New(key)
	}
	if path == "" {
		return nil, errors.New("secretbox: key file path required when no env key is set")
	}
	if raw, err := os.ReadFile(path); err == nil {
		key, perr := ParseKey(string(raw))
		if perr != nil {
			return nil, fmt.Errorf("secretbox: %s: %w", path, perr)
		}
		return New(key)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := writeKeyFile(path, key); err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Info("generated secrets master key", "path", path)
	}
	return New(key)
}

func writeKeyFile(path string, key []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(hex.EncodeToString(key))
	return err
}

// IsSealed reports whether s carries the ciphertext prefix.
func IsSealed(s string) bool { return strings.HasPrefix(s, Prefix) }

// Seal encrypts a plaintext secret. Empty strings pass through and values
// that are already sealed are returned unchanged (idempotent).
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" || IsSealed(plain) {
		return plain, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a sealed secret. Values without the "enc:v1:" prefix are
// legacy plaintext and returned unchanged.
func (b *Box) Open(s string) (string, error) {
	if !IsSealed(s) {
		return s, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, Prefix))
	if err != nil {
		return "", err
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("secretbox: ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
