package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TemplateImageTags returns registry-local tags for a template build.
// Primary (index 0) is name:buildID; always includes name:latest; optional version tags.
func TemplateImageTags(templateName, buildID string, versionTags ...string) []string {
	name := strings.ToLower(strings.TrimSpace(templateName))
	buildID = strings.TrimSpace(buildID)
	if name == "" {
		name = "template"
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(tag string) {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			return
		}
		ref := name + ":" + tag
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	if buildID != "" {
		add(buildID)
	}
	add("latest")
	for _, t := range versionTags {
		if t == "default" || t == "latest" || t == buildID {
			continue
		}
		add(t)
	}
	if len(out) == 0 {
		return []string{name + ":latest"}
	}
	return out
}
