package browser

import (
	"os"
	"path/filepath"
	"strings"
)

// Three-layer browser model:
//
//   Runtime — where Chromium runs (managed container or external CDP).
//   Session — one controllable attach (tabs, takeover, engine lifecycle).
//   Profile — durable user data (cookies / storage) that can outlive a Session.
//
// Settings still describe a CDP Source (provider/endpoint/token/port). A Source
// resolves into a Runtime at attach time. Agent tools keep acting on the
// Session's active Tab only; Tab management is not exposed yet.

// RuntimeKind classifies who owns the Chromium process.
type RuntimeKind string

const (
	RuntimeDocker RuntimeKind = "docker" // Roundpen-managed browserless container
	RuntimeRemote RuntimeKind = "remote" // LAN / self-hosted browserless
	RuntimeCloud  RuntimeKind = "cloud"  // commercial CDP
	RuntimeHost   RuntimeKind = "host"   // roundpend host Chrome
)

// Runtime is one place Chromium runs. Managed docker is Roundpen-owned;
// remote/cloud/host are external endpoints or a host process.
type Runtime struct {
	ID        string      // usually the hub/session key
	Kind      RuntimeKind // docker | remote | cloud | host
	Source    Source      // settings that produced this runtime
	SandboxID string      // dial target when Kind=docker (often == ID)
}

// ResolveRuntime builds a Runtime from a CDP Source and the hub key used to
// attach. For docker, sandboxID is the container to Dial; for external
// providers the key is typically browser-<user>.
func ResolveRuntime(key string, src Source) Runtime {
	kind := RuntimeKind(src.EffectiveProvider())
	rt := Runtime{
		ID:     key,
		Kind:   kind,
		Source: src,
	}
	if kind == RuntimeDocker {
		rt.SandboxID = key
	}
	return rt
}

// Profile is durable browser user data (cookies, localStorage, …) that can
// outlive a Session. Persistence is Roundpen-owned (Playwright storage state
// on the control-plane disk), so the same Profile works with managed docker
// and remote/cloud Runtimes — we do not rely on a remote user-data-dir.
type Profile struct {
	ID string
	// StatePath is where Playwright storage state is read/written.
	// Empty means ephemeral (no cross-session login).
	StatePath string
}

// ProfileIDForUser returns the stable profile id for a Roundpen user.
func ProfileIDForUser(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "default"
	}
	return "user:" + userID
}

// EnsureProfile prepares a per-user Profile under root (usually
// {dataDir}/browser-profiles). The storage-state file is created lazily on
// first successful session close.
func EnsureProfile(root, userID string) (Profile, error) {
	id := ProfileIDForUser(userID)
	dir := filepath.Join(root, sanitizeProfilePath(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Profile{}, err
	}
	return Profile{
		ID:        id,
		StatePath: filepath.Join(dir, "storage.json"),
	}, nil
}

func sanitizeProfilePath(id string) string {
	id = strings.ReplaceAll(id, ":", "_")
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "..", "_")
	if id == "" {
		return "default"
	}
	return id
}

// DefaultTabID is the single active tab until multi-tab is productized.
const DefaultTabID = "main"

// Tab is one page inside a Session. The engine currently drives only the
// active tab; list/switch/close are not exposed to agents yet.
type Tab struct {
	ID string
}

func newMainTab() Tab {
	return Tab{ID: DefaultTabID}
}
