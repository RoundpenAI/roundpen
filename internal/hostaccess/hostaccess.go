// Package hostaccess serves read-only access to granted host directories (NAS
// shares) for the in-process System Agent. Authorization follows the assistant's
// directory grants — the same policy the console advertises — and every read is
// resolved on the control-plane host, so no container mounts are involved.
package hostaccess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/policy"
)

const defaultGlobLimit = 500

// Sessions resolves one agent session.
type Sessions interface {
	Get(ctx context.Context, id string) (*agentsession.Session, error)
}

// Assistants resolves the assistant that owns a session.
type Assistants interface {
	Get(ctx context.Context, id string) (*assistant.Assistant, error)
}

// Access resolves directory grants for a session.
type Access struct {
	sessions   Sessions
	assistants Assistants
	log        *slog.Logger
}

// New builds an accessor. Nil when any dependency is missing, which callers
// treat as "host reads disabled".
func New(sessions Sessions, assistants Assistants, logger *slog.Logger) *Access {
	if sessions == nil || assistants == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Access{sessions: sessions, assistants: assistants, log: logger}
}

// ForSession binds the accessor to one session's assistant grants.
func (a *Access) ForSession(sessionID string) tools.HostFiles {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return &sessionAccess{Access: a, sessionID: sessionID}
}

type sessionAccess struct {
	*Access
	sessionID string
}

func (s *sessionAccess) OpenHost(ctx context.Context, actor tools.Actor, absPath string) (io.ReadCloser, os.FileInfo, error) {
	resolved, err := s.authorize(ctx, actor, absPath)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, fi, nil
}

func (s *sessionAccess) GlobHost(ctx context.Context, actor tools.Actor, rootAbs, pattern string, limit int) ([]string, error) {
	root, err := s.authorize(ctx, actor, rootAbs)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultGlobLimit
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	// Patterns without a separator match file names at any depth (like the
	// guest-side Glob backed by ripgrep).
	basenameOnly := !strings.Contains(pattern, "/")

	var out []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			// Unreadable entries are skipped, not fatal.
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		target := filepath.ToSlash(rel)
		if basenameOnly {
			target = name
		}
		if !MatchGlob(pattern, target) {
			return nil
		}
		out = append(out, p)
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return nil, walkErr
	}
	sort.Strings(out)
	return out, nil
}

// authorize checks the session's assistant grants and returns the symlink-free
// path so a link inside a granted directory cannot escape it.
func (s *sessionAccess) authorize(ctx context.Context, actor tools.Actor, absPath string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(absPath))
	if !strings.HasPrefix(cleaned, "/") || cleaned == "/" {
		return "", fmt.Errorf("host read denied: %q is not an absolute file or directory path", absPath)
	}
	sess, err := s.sessions.Get(ctx, s.sessionID)
	if err != nil {
		return "", fmt.Errorf("host read: session %s: %w", s.sessionID, err)
	}
	if sess == nil {
		return "", fmt.Errorf("host read: session %s not found", s.sessionID)
	}
	if actor.Username == "" || sess.UserID != actor.Username {
		return "", fmt.Errorf("host read denied: session belongs to another user")
	}
	if strings.TrimSpace(sess.AssistantID) == "" {
		return "", fmt.Errorf("host read denied: session has no assistant, so no directory grants apply")
	}
	a, err := s.assistants.Get(ctx, sess.AssistantID)
	if err != nil {
		return "", fmt.Errorf("host read: assistant %s: %w", sess.AssistantID, err)
	}
	prof := assistant.PolicyProfile(a)
	if d := policy.CheckDirectory(prof, cleaned, "read"); !d.Allowed {
		return "", fmt.Errorf("host read denied: %s", d.Reason)
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", err
	}
	if resolved != cleaned {
		if d := policy.CheckDirectory(prof, resolved, "read"); !d.Allowed {
			return "", fmt.Errorf("host read denied: %s resolves outside the granted directories", cleaned)
		}
	}
	return resolved, nil
}

// MatchGlob matches a slash-separated name against a glob pattern: * ? []
// match within one segment, ** spans segments.
func MatchGlob(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pat, seg []string) bool {
	if len(pat) == 0 {
		return len(seg) == 0
	}
	if pat[0] == "**" {
		if matchSegments(pat[1:], seg) {
			return true
		}
		if len(seg) > 0 {
			return matchSegments(pat, seg[1:])
		}
		return false
	}
	if len(seg) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], seg[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], seg[1:])
}
