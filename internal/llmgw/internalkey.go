package llmgw

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func randomVK(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// loadOrGenerateInternalKey reads the per-instance internal virtual key from
// path, generating and persisting it (0600) on first use. An empty path yields
// an ephemeral random key (dev/test).
func loadOrGenerateInternalKey(path string, logger *slog.Logger) string {
	gen := func() string {
		key, err := randomVK("vk-internal-")
		if err != nil {
			logger.Error("generate internal virtual key", slog.Any("err", err))
			return ""
		}
		return key
	}
	if path == "" {
		return gen()
	}
	if data, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key
		}
	}
	key := gen()
	if key == "" {
		return ""
	}
	if err := writeKeyFile(path, key); err != nil {
		logger.Error("persist internal virtual key (using ephemeral key)",
			slog.String("path", path), slog.Any("err", err))
	}
	return key
}

func writeKeyFile(path, key string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(key+"\n"), 0o600)
}

// UserKeyManager issues one virtual key per user for agent sandboxes, so LLM
// traffic is attributable and revocable per user instead of sharing a single
// instance-wide key. Plaintext lives in 0600 files under dir; the DB stores
// only the hash (via Store.UpsertVirtualKey).
type UserKeyManager struct {
	store *Store
	dir   string
	mu    sync.Mutex
}

// NewUserKeyManager builds a manager storing key files under dir.
func NewUserKeyManager(store *Store, dir string) *UserKeyManager {
	return &UserKeyManager{store: store, dir: dir}
}

// KeyFor returns the user's virtual key, generating it on first use.
func (m *UserKeyManager) KeyFor(ctx context.Context, username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", errors.New("llmgw: empty username")
	}
	if m == nil || m.dir == "" || m.store == nil {
		return "", errors.New("llmgw: user key manager not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.dir, "agent-"+keyFileSafe(username)+".key")
	key := ""
	if data, err := os.ReadFile(path); err == nil {
		key = strings.TrimSpace(string(data))
	}
	if key == "" {
		var err error
		if key, err = randomVK("vk-user-"); err != nil {
			return "", err
		}
		if err := writeKeyFile(path, key); err != nil {
			return "", err
		}
	}
	// Seed the vault row when missing (fresh DB, or admin purged the key).
	// DO NOTHING on conflict preserves an admin's disable decision.
	if err := m.store.SeedVirtualKey(VirtualKey{
		Key:       key,
		Name:      "agent-" + username,
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return "", err
	}
	return key, nil
}

// keyFileSafe maps a username to a filename that cannot escape the key dir;
// anything outside [A-Za-z0-9_-] is replaced by a truncated hash.
func keyFileSafe(username string) string {
	encode := func() string {
		sum := sha256.Sum256([]byte(username))
		return hex.EncodeToString(sum[:16])
	}
	if username == "" {
		return encode()
	}
	for i := 0; i < len(username); i++ {
		c := username[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return encode()
		}
	}
	return username
}
