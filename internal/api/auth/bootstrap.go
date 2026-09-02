package auth

import (
	"context"
	"log/slog"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// APIKeyPrefix is prepended to generated API keys.
const APIKeyPrefix = "rp-"

// BootstrapAdmin ensures an admin user exists.
//
// If configuredKey is non-empty it becomes the admin API key (when no user
// already holds that key). If empty and admin does not exist, a random key is
// generated and logged once.
//
// When the admin has no password, a random one is generated and logged once.
func BootstrapAdmin(ctx context.Context, users storage.UserStore, configuredKey string, logger *slog.Logger) error {
	if users == nil {
		return nil
	}
	adminKey := configuredKey
	if adminKey == "" {
		existing, err := users.GetByUsername(ctx, "admin")
		if err == nil && existing != nil {
			logger.Info("admin user already exists")
			return ensureAdminPassword(ctx, users, existing, logger)
		}
		if err != nil && err != storage.ErrNotFound {
			return err
		}
		randPart, err := NewID()
		if err != nil {
			return err
		}
		adminKey = APIKeyPrefix + randPart
		logger.Warn("ROUNDPEN_API_KEY is empty — generated a random admin API key (shown once, save it now!)",
			slog.String("api_key", adminKey))
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
	return ensureAdminPassword(ctx, users, adminUser, logger)
}

func ensureAdminPassword(ctx context.Context, users storage.UserStore, user *storage.User, logger *slog.Logger) error {
	plain, generated, err := EnsurePassword(ctx, users, user)
	if err != nil {
		logger.Warn("admin password bootstrap failed", slog.Any("err", err))
		return err
	}
	if generated {
		logger.Warn("admin initial password (change immediately)", slog.String("password", plain))
	}
	return nil
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
