// Package hostaccess serves granted host directories (NAS shares) for the
// in-process System Agent. Authorization follows the assistant's directory
// grants — the same policy the console advertises — and every op is resolved
// on the control-plane host, so no container mounts are involved.
//
// Reads require a matching grant (read or readwrite). Mutations require
// readwrite on every path they touch.
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
// treat as "host access disabled".
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

func (s *sessionAccess) ListGrants(ctx context.Context, actor tools.Actor) ([]tools.DirGrantView, error) {
	a, err := s.loadAssistant(ctx, actor)
	if err != nil {
		return nil, err
	}
	out := make([]tools.DirGrantView, 0, len(a.DirectoryGrants))
	for _, g := range a.DirectoryGrants {
		mode := strings.TrimSpace(g.Mode)
		if mode == "" {
			mode = "read"
		}
		out = append(out, tools.DirGrantView{Path: g.Path, Mode: mode})
	}
	return out, nil
}

func (s *sessionAccess) OpenHost(ctx context.Context, actor tools.Actor, absPath string) (io.ReadCloser, os.FileInfo, error) {
	resolved, err := s.authorize(ctx, actor, absPath, "read", true)
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
	root, err := s.authorize(ctx, actor, rootAbs, "read", true)
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

func (s *sessionAccess) RenameHost(ctx context.Context, actor tools.Actor, fromAbs, toAbs string) error {
	from, err := s.authorize(ctx, actor, fromAbs, "readwrite", true)
	if err != nil {
		return err
	}
	to, err := s.authorize(ctx, actor, toAbs, "readwrite", false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}

func (s *sessionAccess) MoveHost(ctx context.Context, actor tools.Actor, fromAbs, toAbs string) error {
	from, err := s.authorize(ctx, actor, fromAbs, "readwrite", true)
	if err != nil {
		return err
	}
	to, err := s.authorize(ctx, actor, toAbs, "readwrite", false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	// Cross-device: copy then remove source.
	if err := copyPath(from, to); err != nil {
		return err
	}
	return removePath(from, true)
}

func (s *sessionAccess) CopyHost(ctx context.Context, actor tools.Actor, fromAbs, toAbs string) error {
	from, err := s.authorize(ctx, actor, fromAbs, "read", true)
	if err != nil {
		return err
	}
	to, err := s.authorize(ctx, actor, toAbs, "readwrite", false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return copyPath(from, to)
}

// PublishHost copies a trusted control-plane path (typically the Agent
// workspace host bind) into a readwrite-granted destination. The source is
// not checked against directory grants — callers must already confine it.
func (s *sessionAccess) PublishHost(ctx context.Context, actor tools.Actor, fromAbs, toAbs string) error {
	from := filepath.Clean(strings.TrimSpace(fromAbs))
	if from == "" || from == "." || from == "/" {
		return fmt.Errorf("publish source is required")
	}
	if _, err := os.Lstat(from); err != nil {
		return err
	}
	to, err := s.authorize(ctx, actor, toAbs, "readwrite", false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return copyPath(from, to)
}

func (s *sessionAccess) MkdirHost(ctx context.Context, actor tools.Actor, absPath string) error {
	to, err := s.authorize(ctx, actor, absPath, "readwrite", false)
	if err != nil {
		return err
	}
	return os.MkdirAll(to, 0o755)
}

func (s *sessionAccess) DeleteHost(ctx context.Context, actor tools.Actor, absPath string, recursive bool) error {
	target, err := s.authorize(ctx, actor, absPath, "readwrite", true)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if fi.IsDir() && !recursive {
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("directory is not empty — pass recursive=true to delete it")
		}
	}
	return removePath(target, recursive)
}

func (s *sessionAccess) loadAssistant(ctx context.Context, actor tools.Actor) (*assistant.Assistant, error) {
	sess, err := s.sessions.Get(ctx, s.sessionID)
	if err != nil {
		return nil, fmt.Errorf("host access: session %s: %w", s.sessionID, err)
	}
	if sess == nil {
		return nil, fmt.Errorf("host access: session %s not found", s.sessionID)
	}
	if actor.Username == "" || sess.UserID != actor.Username {
		return nil, fmt.Errorf("host access denied: session belongs to another user")
	}
	if strings.TrimSpace(sess.AssistantID) == "" {
		return nil, fmt.Errorf("host access denied: session has no assistant, so no directory grants apply")
	}
	a, err := s.assistants.Get(ctx, sess.AssistantID)
	if err != nil {
		return nil, fmt.Errorf("host access: assistant %s: %w", sess.AssistantID, err)
	}
	return a, nil
}

// authorize checks grants and returns a path safe to use. When mustExist is
// true the path (after symlink resolution) must already exist. When false,
// missing leaf paths are allowed if an existing ancestor stays inside a grant.
func (s *sessionAccess) authorize(ctx context.Context, actor tools.Actor, absPath, wantMode string, mustExist bool) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(absPath))
	deny := denyVerb(wantMode)
	if !strings.HasPrefix(cleaned, "/") || cleaned == "/" {
		return "", fmt.Errorf("host %s denied: %q is not an absolute file or directory path", deny, absPath)
	}
	a, err := s.loadAssistant(ctx, actor)
	if err != nil {
		return "", err
	}
	prof := assistant.PolicyProfile(a)
	if d := policy.CheckDirectory(prof, cleaned, wantMode); !d.Allowed {
		return "", fmt.Errorf("host %s denied: %s", deny, d.Reason)
	}

	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		if resolved != cleaned {
			if d := policy.CheckDirectory(prof, resolved, wantMode); !d.Allowed {
				return "", fmt.Errorf("host %s denied: %s resolves outside the granted directories", deny, cleaned)
			}
		}
		return resolved, nil
	}
	if mustExist || !pathMissing(err) {
		return "", err
	}

	// Create target: resolve deepest existing ancestor, then re-join the rest.
	parent := cleaned
	var missing []string
	for {
		parent = path.Dir(parent)
		if parent == "/" || parent == "." {
			return "", fmt.Errorf("host %s denied: no existing ancestor under a granted directory for %s", deny, cleaned)
		}
		resolvedParent, perr := filepath.EvalSymlinks(parent)
		if perr == nil {
			if d := policy.CheckDirectory(prof, resolvedParent, wantMode); !d.Allowed {
				return "", fmt.Errorf("host %s denied: %s resolves outside the granted directories", deny, cleaned)
			}
			out := resolvedParent
			for i := len(missing) - 1; i >= 0; i-- {
				out = filepath.Join(out, missing[i])
			}
			out = filepath.Join(out, path.Base(cleaned))
			if d := policy.CheckDirectory(prof, path.Clean(filepath.ToSlash(out)), wantMode); !d.Allowed {
				return "", fmt.Errorf("host %s denied: %s", deny, d.Reason)
			}
			return out, nil
		}
		if !pathMissing(perr) {
			return "", perr
		}
		missing = append(missing, path.Base(parent))
	}
}

func denyVerb(wantMode string) string {
	if wantMode == "readwrite" || wantMode == "write" || wantMode == "rw" {
		return "write"
	}
	return "read"
}

func pathMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return true
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return errors.Is(pe.Err, fs.ErrNotExist) || os.IsNotExist(pe.Err)
	}
	return false
}

func copyPath(from, to string) error {
	fi, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink %s", from)
	}
	if fi.IsDir() {
		return copyDir(from, to)
	}
	return copyFile(from, to, fi.Mode())
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func copyDir(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		src := filepath.Join(from, name)
		dst := filepath.Join(to, name)
		if err := copyPath(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func removePath(target string, recursive bool) error {
	if recursive {
		return os.RemoveAll(target)
	}
	return os.Remove(target)
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
