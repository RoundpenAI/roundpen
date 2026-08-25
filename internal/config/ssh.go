package config

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// SSHTarget is a parsed ssh://user@host[/...] endpoint.
type SSHTarget struct {
	User string
	Host string // host or host:port
}

// String returns user@host for ssh(1).
func (t SSHTarget) String() string {
	if t.User == "" {
		return t.Host
	}
	return t.User + "@" + t.Host
}

// ParseSSHURL parses ssh://user@host or ssh://user@host:port[/path].
func ParseSSHURL(raw string) (SSHTarget, error) {
	if !strings.HasPrefix(raw, "ssh://") {
		return SSHTarget{}, fmt.Errorf("not an ssh URL: %s", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return SSHTarget{}, err
	}
	host := u.Host
	if host == "" {
		return SSHTarget{}, fmt.Errorf("ssh URL missing host: %s", raw)
	}
	user := ""
	if u.User != nil {
		user = u.User.Username()
	}
	return SSHTarget{User: user, Host: host}, nil
}

// IsSSHDockerHost reports whether DockerHost uses the ssh:// scheme.
func (c *Config) IsSSHDockerHost() bool {
	return strings.HasPrefix(c.DockerHost, "ssh://")
}

// DockerSSHTarget returns the SSH target for a remote Docker host.
func (c *Config) DockerSSHTarget() (SSHTarget, error) {
	if !c.IsSSHDockerHost() {
		return SSHTarget{}, fmt.Errorf("DOCKER_HOST is not ssh://")
	}
	return ParseSSHURL(c.DockerHost)
}

// EffectiveDataRoot returns the workspace root path.
// For ssh:// Docker hosts, relative roots (e.g. ./data) are remapped to a
// remote absolute default because bind mounts are resolved on the Docker host.
func (c *Config) EffectiveDataRoot() string {
	root := c.DataRoot
	if c.Backend == "docker" && c.IsSSHDockerHost() {
		if root == "" || root == "./data" || !path.IsAbs(root) {
			return "/var/lib/roundpen"
		}
	}
	return root
}

// WorkspaceUsesSSH is true when the control plane must talk to a remote FS
// (remote Docker over SSH).
func (c *Config) WorkspaceUsesSSH() bool {
	return c.Backend == "docker" && c.IsSSHDockerHost()
}
