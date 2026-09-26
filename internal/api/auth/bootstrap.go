package auth

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// APIKeyPrefix is prepended to generated API keys.
const APIKeyPrefix = "rp-"

// BootstrapAdmin ensures an admin user exists.
//
// If configuredKey is non-empty it becomes the admin API key (when no user
// already holds that key). If empty and admin does not exist, a random key is
// generated. A non-empty configuredPassword pins the admin password on every
// boot; otherwise a random one is generated when the admin has none.
//
// Generated credentials are written to credFile (0600) instead of the log;
// only masked values and the file path appear in log output.
func BootstrapAdmin(ctx context.Context, users storage.UserStore, configuredKey, configuredPassword, credFile string, logger *slog.Logger) error {
	if users == nil {
		return nil
	}
	adminKey := configuredKey
	if adminKey == "" {
		existing, err := users.GetByUsername(ctx, "admin")
		if err == nil && existing != nil {
			logger.Info("admin user already exists")
			return ensureAdminPassword(ctx, users, existing, configuredPassword, credFile, logger)
		}
		if err != nil && err != storage.ErrNotFound {
			return err
		}
		randPart, err := NewID()
		if err != nil {
			return err
		}
		adminKey = APIKeyPrefix + randPart
		if err := appendCredFile(credFile, "admin API key: "+adminKey); err != nil {
			return err
		}
		logger.Warn("ROUNDPEN_API_KEY is empty — generated a random admin API key (save it now!)",
			slog.String("api_key", maskAPIKey(adminKey)),
			slog.String("credentials_file", credFile))
	}

	masked := maskAPIKey(adminKey)
	existing, err := users.GetByAPIKey(ctx, adminKey)
	if err != nil {
		if err != storage.ErrNotFound {
			return err
		}
		logger.Info("creating default admin user", slog.String("api_key", masked))
		admin := storage.User{
			Username:     "admin",
			Email:        "admin@example.com",
			FullName:     "System Administrator",
			APIKey:       adminKey,
			Role:         storage.RoleAdmin,
			AuthProvider: "local",
		}
		if err := users.Upsert(ctx, admin); err != nil {
			return err
		}
		logger.Info("admin user created", slog.String("username", "admin"), slog.String("api_key", masked))
	} else {
		logger.Info("admin user already exists for API key", slog.String("username", existing.Username), slog.String("api_key", masked))
	}

	adminUser, err := users.GetByUsername(ctx, "admin")
	if err != nil {
		if err == storage.ErrNotFound {
			return nil
		}
		return err
	}
	return ensureAdminPassword(ctx, users, adminUser, configuredPassword, credFile, logger)
}

func ensureAdminPassword(ctx context.Context, users storage.UserStore, user *storage.User, configuredPassword, credFile string, logger *slog.Logger) error {
	if configuredPassword != "" {
		return pinAdminPassword(ctx, users, user, configuredPassword, logger)
	}
	plain, generated, err := EnsurePassword(ctx, users, user)
	if err != nil {
		logger.Warn("admin password bootstrap failed", slog.Any("err", err))
		return err
	}
	if generated {
		if err := appendCredFile(credFile, "admin initial password: "+plain); err != nil {
			return err
		}
		logger.Warn("admin initial password generated (change immediately)",
			slog.String("credentials_file", credFile))
	}
	return nil
}

// pinAdminPassword keeps the admin password equal to
// ROUNDPEN_BOOTSTRAP_ADMIN_PASSWORD. The stored hash is only rewritten when it
// no longer matches, so a password changed through the API is reset on the next
// boot — that is the point of the setting, and it is meant for local dev.
func pinAdminPassword(ctx context.Context, users storage.UserStore, user *storage.User, plain string, logger *slog.Logger) error {
	if len(plain) < minPasswordLen {
		return fmt.Errorf("ROUNDPEN_BOOTSTRAP_ADMIN_PASSWORD: %w", errPasswordTooShort)
	}
	if CheckPassword(user.PasswordHash, plain) {
		return nil
	}
	hash, err := HashPassword(plain)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	if user.AuthProvider == "" {
		user.AuthProvider = "local"
	}
	if err := users.Upsert(ctx, *user); err != nil {
		return err
	}
	logger.Warn("admin password pinned by ROUNDPEN_BOOTSTRAP_ADMIN_PASSWORD")
	return nil
}

// appendCredFile writes one line to a 0600 file so generated bootstrap
// credentials never end up in structured logs.
func appendCredFile(path, line string) error {
	if path == "" {
		return fmt.Errorf("credentials file path is empty")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

// EnsurePassword sets a random password when the user has none. Returns the
// plaintext only when a new password was generated.
func EnsurePassword(ctx context.Context, users storage.UserStore, user *storage.User) (plain string, generated bool, err error) {
	if user == nil || user.PasswordHash != "" {
		return "", false, nil
	}
	plain, err = randomPassword(16)
	if err != nil {
		return "", false, err
	}
	hash, err := HashPassword(plain)
	if err != nil {
		return "", false, err
	}
	user.PasswordHash = hash
	if user.AuthProvider == "" {
		user.AuthProvider = "local"
	}
	if err := users.Upsert(ctx, *user); err != nil {
		return "", false, err
	}
	return plain, true, nil
}
