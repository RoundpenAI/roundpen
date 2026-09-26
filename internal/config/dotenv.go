package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// LoadDotEnv fills process environment variables from a KEY=VALUE file (usually
// ./.env next to the binary). Values already present in the environment win, so
// systemd / compose / `FOO=bar ./roundpend` keep working; an empty value counts
// as unset, matching getenv. A missing file is not an error.
func LoadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, i+1)
		}
		key = strings.TrimSpace(key)
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, unquote(strings.TrimSpace(val))); err != nil {
			return fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
	}
	return nil
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
