// Package userworkspace stores the directory each user chose in their own
// files. That directory is never a shared app folder.
package userworkspace

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// Store persists one absolute directory per user.
type Store struct {
	DB *sql.DB
}

// Get returns the bound directory, or "" when the user has not chosen one.
func (s *Store) Get(ctx context.Context, userID string) (string, error) {
	if s == nil || s.DB == nil {
		return "", nil
	}
	var path string
	err := s.DB.QueryRowContext(ctx, `SELECT path FROM user_workspace_roots WHERE user_id=$1`, userID).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// Put saves path for userID.
func (s *Store) Put(ctx context.Context, userID, path string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_workspace_roots (user_id, path, updated_at)
		VALUES ($1,$2,now())
		ON CONFLICT (user_id) DO UPDATE SET path=EXCLUDED.path, updated_at=now()`,
		userID, path)
	return err
}

// Delete removes the binding. It does not delete the directory.
func (s *Store) Delete(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_workspace_roots WHERE user_id=$1`, userID)
	return err
}

// FnosUID returns the numeric uid encoded in a gateway username (fnos-1000).
func FnosUID(authProvider, username string) (int, bool) {
	if authProvider != "fnos" {
		return 0, false
	}
	rest := strings.TrimPrefix(username, "fnos-")
	if rest == username || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// AcceptPath rejects empty, relative, and app-shared locations.
// A shared tree would show every user's files to anyone who can open it.
func AcceptPath(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || !filepath.IsAbs(path) {
		return "", errors.New("choose a folder in your own files")
	}
	if path == "/" || strings.Contains(path, "/@appshare") || strings.Contains(path, "/@appdata") || strings.Contains(path, "/@appcenter") {
		return "", errors.New("that folder is shared by the app; choose one under your own files")
	}
	return path, nil
}

// Covers reports whether path is the authorized folder or inside it.
func Covers(folders []string, path string) bool {
	path = filepath.Clean(path)
	for _, folder := range folders {
		folder = filepath.Clean(strings.TrimSpace(folder))
		if folder == "" {
			continue
		}
		if path == folder || strings.HasPrefix(path, folder+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
