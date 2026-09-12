package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// workspaceCopier is implemented by Docker (and multi forwarding to Docker).
type workspaceCopier interface {
	CopyToWorkspace(ctx context.Context, sandboxID, destRel string, r io.Reader) error
	CopyFromWorkspace(ctx context.Context, sandboxID, srcRel string) (io.ReadCloser, error)
}

// guestRel normalizes a workspace-relative path and rejects escapes.
// Empty or "." means the workspace root (list only).
func guestRel(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return ".", nil
	}
	rel = strings.TrimPrefix(rel, "/")
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("path escapes workspace")
	}
	if clean == "." {
		return ".", nil
	}
	if strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return clean, nil
}

func guestAbs(rel string) (string, error) {
	r, err := guestRel(rel)
	if err != nil {
		return "", err
	}
	if r == "." {
		return "/workspace", nil
	}
	return path.Join("/workspace", r), nil
}

func (s *Service) requireRunning(ctx context.Context, id string) (*Sandbox, error) {
	sb, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if sb.Status != StatusRunning && sb.Status != StatusCreating {
		return nil, fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
	if err := s.hydrate(ctx, sb); err != nil {
		return nil, err
	}
	return sb, nil
}

func (s *Service) workspaceCopier() (workspaceCopier, error) {
	if c, ok := s.backend.(workspaceCopier); ok {
		return c, nil
	}
	return nil, fmt.Errorf("guest workspace IO requires Docker")
}

// ListGuestFiles lists entries under /workspace via container exec.
func (s *Service) ListGuestFiles(ctx context.Context, id, rel string) ([]workspace.DirEntry, error) {
	if _, err := s.requireRunning(ctx, id); err != nil {
		return nil, err
	}
	abs, err := guestAbs(rel)
	if err != nil {
		return nil, err
	}
	script := `import json,os,sys
p=sys.argv[1]
out=[]
for name in sorted(os.listdir(p)):
    fp=os.path.join(p,name)
    try:
        st=os.stat(fp, follow_symlinks=False)
        out.append({"name":name,"is_dir":os.path.isdir(fp),"size":st.st_size})
    except OSError:
        continue
print(json.dumps(out))`
	res, err := s.backend.Exec(ctx, id, backend.ExecOpts{
		Cmd:     []string{"python3", "-c", script, abs},
		WorkDir: "/workspace",
		Timeout: 0,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(res.Stdout))
		}
		if msg == "" {
			msg = "list failed"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var raw []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}
	if err := json.Unmarshal(res.Stdout, &raw); err != nil {
		return nil, fmt.Errorf("list parse: %w", err)
	}
	out := make([]workspace.DirEntry, 0, len(raw))
	for _, e := range raw {
		out = append(out, workspace.DirEntry{Name: e.Name, IsDir: e.IsDir, Size: e.Size})
	}
	return out, nil
}

// ReadGuestFile streams a file from /workspace via Docker copy.
func (s *Service) ReadGuestFile(ctx context.Context, id, rel string) (io.ReadCloser, error) {
	if _, err := s.requireRunning(ctx, id); err != nil {
		return nil, err
	}
	r, err := guestRel(rel)
	if err != nil {
		return nil, err
	}
	if r == "." {
		return nil, fmt.Errorf("path is required")
	}
	c, err := s.workspaceCopier()
	if err != nil {
		return nil, err
	}
	return c.CopyFromWorkspace(ctx, id, r)
}

// WriteGuestFile writes a file into /workspace via Docker copy.
func (s *Service) WriteGuestFile(ctx context.Context, id, rel string, r io.Reader) error {
	if _, err := s.requireRunning(ctx, id); err != nil {
		return err
	}
	dest, err := guestRel(rel)
	if err != nil {
		return err
	}
	if dest == "." {
		return fmt.Errorf("path is required")
	}
	c, err := s.workspaceCopier()
	if err != nil {
		return err
	}
	return c.CopyToWorkspace(ctx, id, dest, r)
}

// RemoveGuestFile deletes a file or directory under /workspace via container exec.
func (s *Service) RemoveGuestFile(ctx context.Context, id, rel string) error {
	if _, err := s.requireRunning(ctx, id); err != nil {
		return err
	}
	r, err := guestRel(rel)
	if err != nil {
		return err
	}
	if r == "." {
		return fmt.Errorf("path is required")
	}
	abs, err := guestAbs(r)
	if err != nil {
		return err
	}
	res, err := s.backend.Exec(ctx, id, backend.ExecOpts{
		Cmd:     []string{"rm", "-rf", "--", abs},
		WorkDir: "/workspace",
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = "remove failed"
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
