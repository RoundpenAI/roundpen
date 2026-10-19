package toolgw

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/policy"
)

// checkPolicy returns deny=true when an assistant profile forbids the call.
// nil profile = allow (zero-config NAS default).
func checkPolicy(p *policy.Profile, name string, args json.RawMessage) (deny bool, rule, reason string) {
	if p == nil {
		return false, "", ""
	}
	name = strings.TrimSpace(name)

	if capName := capabilityForTool(name); capName != "" {
		d := policy.CheckCapability(p, capName)
		if !d.Allowed {
			return true, "capability:" + d.Target, d.Reason
		}
	}

	for _, u := range extractURLs(name, args) {
		d := policy.CheckNetwork(p, u)
		if !d.Allowed {
			return true, "network:" + d.Target, d.Reason
		}
	}

	if path, mode, ok := extractHostPath(name, args); ok {
		d := policy.CheckDirectory(p, path, mode)
		if !d.Allowed {
			return true, "directory:" + d.Target, d.Reason
		}
	}
	return false, "", ""
}

func capabilityForTool(name string) string {
	switch {
	case name == "Bash" || name == "KillShell":
		return "shell"
	case strings.HasPrefix(name, "browser_"):
		return "browser"
	default:
		return ""
	}
}

var (
	jsonURLKey = regexp.MustCompile(`(?i)"(url|uri|href|endpoint)"\s*:\s*"([^"]+)"`)
	bareURL    = regexp.MustCompile(`(?i)https?://[^\s"'\\]+`)
)

func extractURLs(name string, args json.RawMessage) []string {
	raw := string(args)
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, m := range jsonURLKey.FindAllStringSubmatch(raw, 8) {
		if len(m) >= 3 {
			add(m[2])
		}
	}
	// Bash / explore / navigate often embed URLs in free text.
	if name == "Bash" || strings.HasPrefix(name, "browser_") || name == "WebFetch" {
		for _, u := range bareURL.FindAllString(raw, 8) {
			add(u)
		}
	}
	return out
}

func extractHostPath(name string, args json.RawMessage) (path, mode string, ok bool) {
	switch name {
	case "Read", "Glob", "Grep":
		mode = "read"
	case "Write", "Edit":
		mode = "readwrite"
	default:
		return "", "", false
	}
	var in struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", "", false
	}
	path = strings.TrimSpace(in.Path)
	if path == "" {
		path = strings.TrimSpace(in.File)
	}
	if path == "" {
		return "", "", false
	}
	// Workspace-relative paths are always allowed by CheckDirectory.
	return path, mode, true
}
