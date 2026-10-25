package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HasGitRepo reports whether projects/<slug> already contains a .git directory.
func HasGitRepo(hostPath, slug string) bool {
	if !ValidProjectSlug(slug) {
		return false
	}
	git := filepath.Join(filepath.Clean(hostPath), filepath.FromSlash(ProjectRelPath(slug)), ".git")
	st, err := os.Stat(git)
	return err == nil && st.IsDir()
}

// CloneScript is run inside the agent guest to clone repoURL into projects/<slug>.
// Arg $1 = slug, $2 = clone URL. Preserves an existing .roundpen/ tree.
func CloneScript() string {
	return `set -e
SLUG="$1"
URL="$2"
DEST="/workspace/projects/$SLUG"
if [ -z "$SLUG" ] || [ -z "$URL" ]; then
  echo "slug and url required" >&2
  exit 2
fi
if [ -d "$DEST/.git" ]; then
  echo "already_cloned"
  exit 0
fi
CTX=""
if [ -d "$DEST/.roundpen" ]; then
  CTX=$(mktemp -d)
  cp -a "$DEST/.roundpen" "$CTX/"
fi
# Remove scaffold so git clone can create DEST.
rm -rf "$DEST"
mkdir -p /workspace/projects
git clone -- "$URL" "$DEST"
if [ -n "$CTX" ] && [ -d "$CTX/.roundpen" ]; then
  cp -a "$CTX/.roundpen" "$DEST/"
  rm -rf "$CTX"
fi
mkdir -p "$DEST/.roundpen/context"
if [ ! -f "$DEST/.roundpen/context/README.md" ]; then
  printf '%s\n' "# Project context" "" "Shared notes for agents working on this project." > "$DEST/.roundpen/context/README.md"
fi
echo "cloned"
`
}

// ValidateCloneArgs checks slug + URL before invoking the guest script.
func ValidateCloneArgs(slug, repoURL string) error {
	if !ValidProjectSlug(slug) {
		return fmt.Errorf("invalid project slug %q", slug)
	}
	if strings.TrimSpace(repoURL) == "" {
		return fmt.Errorf("repo url is required")
	}
	return nil
}
