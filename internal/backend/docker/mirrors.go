package docker

import (
	"strings"
	"sync"
)

var (
	preferredMu sync.Mutex
	// preferredPull remembers which candidate succeeded for a logical ref
	// so subsequent pulls in this process try the working mirror first.
	preferredPull = map[string]string{}
)

// ParseImageMirrors reads ROUNDPEN_IMAGE_MIRRORS.
// "none" / "off" / "-" disables built-in fallbacks and clears custom list.
// Otherwise entries are comma-separated templates or hosts (see expandMirror).
func ParseImageMirrors(raw string) (custom []string, disableBuiltin bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	switch strings.ToLower(raw) {
	case "none", "off", "-", "false", "0":
		return nil, true
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		custom = append(custom, part)
	}
	return custom, false
}

// pullCandidates returns unique refs to try when materializing logicalRef
// locally. Order: remembered preferred → original → custom → built-ins.
//
// Built-ins target registries that Docker's daemon.json registry-mirrors
// cannot accelerate (ghcr.io). DaoCloud public-image-mirror is the default
// China-friendly fallback; operators can override via ROUNDPEN_IMAGE_MIRRORS.
func pullCandidates(logicalRef string, custom []string, disableBuiltin bool) []string {
	logicalRef = strings.TrimSpace(logicalRef)
	if logicalRef == "" {
		return nil
	}
	var out []string
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		for _, existing := range out {
			if existing == ref {
				return
			}
		}
		out = append(out, ref)
	}

	preferredMu.Lock()
	pref := preferredPull[logicalRef]
	preferredMu.Unlock()
	if pref != "" {
		add(pref)
	}
	add(logicalRef)

	for _, tmpl := range custom {
		add(expandMirror(tmpl, logicalRef))
	}
	if !disableBuiltin {
		for _, tmpl := range builtinMirrorsFor(logicalRef) {
			add(expandMirror(tmpl, logicalRef))
		}
	}
	return out
}

func builtinMirrorsFor(logicalRef string) []string {
	registry, _, ok := splitRegistry(logicalRef)
	if !ok {
		return nil
	}
	switch registry {
	case "ghcr.io":
		// Order: DaoCloud (prefix + replace) → 1ms.run → NJU cache.
		// DaoCloud is widely used but rate-limited and has had outages;
		// 1ms/NJU are alternate China paths (NJU is a university cache and
		// may lag on brand-new tags).
		return []string{
			"m.daocloud.io/{image}",
			"ghcr.m.daocloud.io/{path}",
			"ghcr.1ms.run/{path}",
			"ghcr.nju.edu.cn/{path}",
		}
	case "docker.io":
		return []string{
			"m.daocloud.io/{image}",
			"docker.m.daocloud.io/{path}",
			"docker.1ms.run/{path}",
		}
	default:
		return []string{"m.daocloud.io/{image}"}
	}
}

func rememberPreferredPull(logicalRef, pullRef string) {
	logicalRef = strings.TrimSpace(logicalRef)
	pullRef = strings.TrimSpace(pullRef)
	if logicalRef == "" || pullRef == "" || pullRef == logicalRef {
		return
	}
	preferredMu.Lock()
	preferredPull[logicalRef] = pullRef
	preferredMu.Unlock()
}

func expandMirror(tmpl, logicalRef string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	registry, path, ok := splitRegistry(logicalRef)
	if !ok {
		return ""
	}
	if strings.Contains(tmpl, "{image}") || strings.Contains(tmpl, "{path}") {
		out := strings.ReplaceAll(tmpl, "{image}", logicalRef)
		out = strings.ReplaceAll(out, "{path}", path)
		return strings.TrimPrefix(strings.TrimPrefix(out, "https://"), "http://")
	}
	// Bare host: DaoCloud-style registry replace when host looks like a
	// per-registry proxy (ghcr.m.… → drop ghcr.io/), otherwise prefix the
	// full original ref (m.daocloud.io/ghcr.io/…).
	host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(tmpl, "https://"), "http://"), "/")
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "ghcr.") || strings.Contains(host, ".ghcr.") {
		if registry != "ghcr.io" {
			return ""
		}
		return host + "/" + path
	}
	if strings.HasPrefix(host, "docker.") && strings.Contains(host, "daocloud") {
		if registry != "docker.io" {
			return ""
		}
		return host + "/" + path
	}
	return host + "/" + logicalRef
}

func splitRegistry(ref string) (registry, path string, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "://") {
		return "", "", false
	}
	slash := strings.IndexByte(ref, '/')
	if slash <= 0 {
		// short name like "ubuntu" → docker.io/library/ubuntu
		return "docker.io", "library/" + ref, true
	}
	host := ref[:slash]
	rest := ref[slash+1:]
	if !strings.Contains(host, ".") && host != "localhost" && !strings.Contains(host, ":") {
		// org/name on Docker Hub
		return "docker.io", ref, true
	}
	return host, rest, rest != ""
}
