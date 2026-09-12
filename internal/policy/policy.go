// Package policy evaluates assistant constitution boundaries.
// Soft denials are returned to the agent as appliable; they do NOT auto-page humans.
package policy

import (
	"net/url"
	"path"
	"strings"
)

const (
	NetworkNone     = "none"
	NetworkDevSites = "dev_sites"
	NetworkAll      = "all"
)

// Caps mirrors assistant capability switches.
type Caps struct {
	Shell   bool
	Browser bool
	Mobile  bool
	Desktop bool
}

// DirGrant is a host directory authorization.
type DirGrant struct {
	Path string
	Mode string // read | readwrite
}

// Profile is the constitution slice needed for checks (avoids importing assistant).
type Profile struct {
	Capabilities     Caps
	NetworkTier      string
	NetworkAllowlist []string
	DirectoryGrants  []DirGrant
}

// Decision is the result of a policy check.
type Decision struct {
	Allowed   bool   `json:"allowed"`
	Dimension string `json:"dimension"` // network | directory | capability
	Target    string `json:"target"`
	Reason    string `json:"reason"`
	Appliable bool   `json:"appliable"` // agent may raise an assist ticket
}

// DevSites is the platform default allowlist for network_tier=dev_sites.
var DevSites = []string{
	"github.com",
	"githubusercontent.com",
	"gitlab.com",
	"bitbucket.org",
	"npmjs.com",
	"npmjs.org",
	"registry.npmjs.org",
	"pypi.org",
	"files.pythonhosted.org",
	"proxy.golang.org",
	"sum.golang.org",
	"pkg.go.dev",
	"golang.org",
	"google.com",
	"googleapis.com",
	"gcr.io",
	"docker.io",
	"ghcr.io",
	"anthropic.com",
	"api.anthropic.com",
	"openai.com",
	"api.openai.com",
	"cursor.com",
	"crates.io",
	"static.crates.io",
}

// CheckCapability returns whether a capability switch is on.
func CheckCapability(p *Profile, name string) Decision {
	name = strings.ToLower(strings.TrimSpace(name))
	d := Decision{Dimension: "capability", Target: name, Appliable: true}
	if p == nil {
		d.Reason = "assistant missing"
		return d
	}
	var on bool
	switch name {
	case "shell", "terminal":
		on = p.Capabilities.Shell
		d.Target = "shell"
	case "browser":
		on = p.Capabilities.Browser
	case "mobile":
		on = p.Capabilities.Mobile
		d.Appliable = false
	case "desktop":
		on = p.Capabilities.Desktop
		d.Appliable = false
	default:
		d.Reason = "unknown capability"
		d.Appliable = false
		return d
	}
	if on {
		d.Allowed = true
		return d
	}
	d.Reason = "能力未开启（策略禁止，可申请打开）"
	return d
}

// CheckNetwork evaluates host access against network tier + allowlist.
func CheckNetwork(p *Profile, rawURL string) Decision {
	d := Decision{Dimension: "network", Target: rawURL, Appliable: true}
	if p == nil {
		d.Reason = "assistant missing"
		return d
	}
	host := extractHost(rawURL)
	d.Target = host
	if host == "" {
		d.Reason = "invalid host"
		d.Appliable = false
		return d
	}
	switch p.NetworkTier {
	case NetworkAll:
		d.Allowed = true
		return d
	case NetworkNone:
		d.Reason = "网络策略为禁止上网（可申请放行）"
		return d
	default:
		if hostAllowed(host, DevSites) || hostAllowed(host, p.NetworkAllowlist) {
			d.Allowed = true
			return d
		}
		d.Reason = "不在常用开发站白名单（可申请允许一次或加入白名单）"
		return d
	}
}

// CheckDirectory evaluates host-path access against grants.
func CheckDirectory(p *Profile, hostPath, wantMode string) Decision {
	d := Decision{Dimension: "directory", Target: hostPath, Appliable: true}
	if p == nil {
		d.Reason = "assistant missing"
		return d
	}
	cleaned := path.Clean(strings.TrimSpace(hostPath))
	d.Target = cleaned
	if cleaned == "" || cleaned == "." || cleaned == "/workspace" || strings.HasPrefix(cleaned, "/workspace/") {
		d.Allowed = true
		return d
	}
	wantWrite := wantMode == "readwrite" || wantMode == "write" || wantMode == "rw"
	for _, g := range p.DirectoryGrants {
		root := path.Clean(g.Path)
		if cleaned == root || strings.HasPrefix(cleaned, root+"/") {
			if !wantWrite || g.Mode == "readwrite" {
				d.Allowed = true
				return d
			}
			d.Reason = "目录仅为只读授权（可申请读写）"
			return d
		}
	}
	d.Reason = "未授权访问该本机目录（可申请授权）"
	return d
}

func extractHost(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func hostAllowed(host string, list []string) bool {
	host = strings.ToLower(host)
	for _, entry := range list {
		e := strings.ToLower(strings.TrimSpace(entry))
		e = strings.TrimPrefix(e, "*.")
		if e == "" {
			continue
		}
		if host == e || strings.HasSuffix(host, "."+e) {
			return true
		}
	}
	return false
}
