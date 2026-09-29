package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// MaxHostGlobResults caps host directory listings; the tool tells the model to
// narrow the pattern when the cap is hit.
const MaxHostGlobResults = 500

// HostFiles reads granted host directories (NAS shares) on the control-plane
// host. Implementations own authorization: each call resolves the actor's
// session assistant and checks its directory grants. Reads are read-only.
type HostFiles interface {
	// OpenHost opens absPath and returns its handle and metadata.
	OpenHost(ctx context.Context, actor Actor, absPath string) (io.ReadCloser, os.FileInfo, error)
	// GlobHost lists at most limit files under rootAbs matching pattern.
	GlobHost(ctx context.Context, actor Actor, rootAbs, pattern string, limit int) ([]string, error)
}

// hostReservedPrefixes are virtual filesystems that never hold user data.
var hostReservedPrefixes = []string{"/proc", "/sys", "/dev"}

// hostAbsPath reports whether p names a granted-host path (absolute, outside
// /workspace) and returns its cleaned form.
func hostAbsPath(p string) (string, bool, error) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		return "", false, nil
	}
	if strings.ContainsRune(p, 0) {
		return "", false, fmt.Errorf("path must not contain NUL")
	}
	clean := path.Clean(p)
	if clean == WorkspaceRoot || strings.HasPrefix(clean, WorkspaceRoot+"/") {
		return "", false, nil
	}
	if clean == "/" {
		return "", false, fmt.Errorf("host path must name a file or directory, not /")
	}
	for _, prefix := range hostReservedPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return "", false, fmt.Errorf("host path %s is not readable", clean)
		}
	}
	return clean, true, nil
}

func (b *AgentBinder) readHostFile(ctx context.Context, actor Actor, absPath string, offset, limit int) (string, error) {
	if b.Host == nil {
		return "", fmt.Errorf("host directories are not available in this session")
	}
	rc, fi, err := b.Host.OpenHost(ctx, actor, absPath)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	if fi != nil && fi.IsDir() {
		return "", fmt.Errorf("%s is a directory — use Glob with path=%q to list it", absPath, absPath)
	}
	raw, truncated, err := readCapped(rc)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "File is empty.", nil
	}
	return formatText(raw, truncated, offset, limit), nil
}

func (b *AgentBinder) globHost(ctx context.Context, actor Actor, rootAbs, pattern string) (string, error) {
	if b.Host == nil {
		return "", fmt.Errorf("host directories are not available in this session")
	}
	matches, err := b.Host.GlobHost(ctx, actor, rootAbs, pattern, MaxHostGlobResults)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "No files matched.", nil
	}
	out := strings.Join(matches, "\n")
	if len(matches) >= MaxHostGlobResults {
		out += fmt.Sprintf("\n\n[stopped at %d matches; narrow the pattern]", MaxHostGlobResults)
	}
	return truncateRunes(out, maxExecOutput), nil
}

// readCapped reads at most maxReadBytes and reports whether the source had more.
func readCapped(r io.Reader) (raw []byte, truncated bool, err error) {
	raw, err = io.ReadAll(io.LimitReader(r, maxReadBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > maxReadBytes {
		raw = raw[:maxReadBytes]
		truncated = true
	}
	return raw, truncated, nil
}

// formatText renders file bytes with 1-based line numbers, honoring offset/limit.
func formatText(raw []byte, truncated bool, offset, limit int) string {
	lines := strings.Split(string(raw), "\n")
	// Preserve trailing newline semantics: Split keeps a final empty element when text ends with \n.
	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start >= len(lines) {
		return fmt.Sprintf("Offset %d is past the end of the file (%d lines).", offset, len(lines))
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	var bld strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&bld, "%6d|%s\n", i+1, lines[i])
	}
	out := strings.TrimSuffix(bld.String(), "\n")
	if truncated {
		out += "\n\n[File truncated at size limit; use offset/limit to read more.]"
	}
	return out
}
