package sandbox

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

// MoveGuestFile moves (or renames) one workspace entry inside the container.
// dest may be an existing directory — the entry then lands inside it under its
// own name — or the literal target path (a same-directory rename).
func (s *Service) MoveGuestFile(ctx context.Context, id, src, dest string) error {
	return s.relocateGuestFile(ctx, id, src, dest, false)
}

// CopyGuestFile copies one workspace entry into dest with cp -a (attributes
// preserved).
func (s *Service) CopyGuestFile(ctx context.Context, id, src, dest string) error {
	return s.relocateGuestFile(ctx, id, src, dest, true)
}

func (s *Service) relocateGuestFile(ctx context.Context, id, src, dest string, isCopy bool) error {
	if _, err := s.requireRunning(ctx, id); err != nil {
		return err
	}
	srcRel, err := guestRel(src)
	if err != nil {
		return err
	}
	if srcRel == "." {
		return fmt.Errorf("path is required")
	}
	destRel, err := guestRel(dest)
	if err != nil {
		return err
	}
	srcAbs, err := guestAbs(srcRel)
	if err != nil {
		return err
	}
	destAbs, err := guestAbs(destRel)
	if err != nil {
		return err
	}

	// An existing directory receives the entry under its own name; anything
	// else is the literal target (rename, or a same-directory no-op).
	if ok, err := s.guestTest(ctx, id, "-d", destAbs); err != nil {
		return err
	} else if ok {
		destRel = path.Join(destRel, path.Base(srcRel))
		destAbs = path.Join(destAbs, path.Base(srcRel))
	}
	if destRel == srcRel {
		// Pasting a cut entry back where it came from is a silent no-op, but
		// copying onto itself would pretend a second copy exists — that is a
		// conflict like any other occupied target.
		if isCopy {
			return fmt.Errorf("%w: %q already exists", ErrConflict, destRel)
		}
		return nil
	}
	if strings.HasPrefix(destRel+"/", srcRel+"/") {
		return fmt.Errorf("cannot place %q inside itself", srcRel)
	}
	if ok, err := s.guestTest(ctx, id, "-e", destAbs); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("%w: %q already exists", ErrConflict, destRel)
	}

	cmd := []string{"mv", "--", srcAbs, destAbs}
	if isCopy {
		cmd = []string{"cp", "-a", "--", srcAbs, destAbs}
	}
	res, err := s.backend.Exec(ctx, id, backend.ExecOpts{Cmd: cmd, WorkDir: "/workspace"})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return classifyGuestExecErr(res.Stderr)
	}
	return nil
}

// guestTest runs a `test` probe in the container and reports whether it
// succeeded.
func (s *Service) guestTest(ctx context.Context, id, op, abs string) (bool, error) {
	res, err := s.backend.Exec(ctx, id, backend.ExecOpts{
		Cmd:     []string{"test", op, abs},
		WorkDir: "/workspace",
	})
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

// classifyGuestExecErr maps mv/cp stderr onto the shared sentinel errors so
// the HTTP layer can answer 404/409 without parsing shell text itself.
func classifyGuestExecErr(stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "no such file"), strings.Contains(low, "not found"):
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	case strings.Contains(low, "exist"), strings.Contains(low, "not empty"):
		return fmt.Errorf("%w: %s", ErrConflict, msg)
	}
	if msg == "" {
		msg = "file operation failed"
	}
	return fmt.Errorf("%s", msg)
}
