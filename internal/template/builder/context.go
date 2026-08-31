package builder

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteBuildContext creates a temp directory containing a Dockerfile.
func WriteBuildContext(dockerfile string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "roundpen-template-build-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(dockerfile), 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

// DirContextURI returns a Kaniko dir:// context URI for a local path.
func DirContextURI(dir string) string {
	return "dir://" + dir
}

// SanitizeImageRef strips a trailing slash from registry prefixes.
func SanitizeImageRef(ref string) string {
	for len(ref) > 0 && ref[len(ref)-1] == '/' {
		ref = ref[:len(ref)-1]
	}
	if ref == "" {
		return ref
	}
	return ref
}

// JoinImageRef joins registry prefix and image name safely.
func JoinImageRef(prefix, name string) string {
	prefix = SanitizeImageRef(prefix)
	if prefix == "" {
		return name
	}
	return fmt.Sprintf("%s/%s", prefix, name)
}
