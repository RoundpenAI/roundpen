package automode

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// PreCheck applies deterministic L3 HardDeny rules before the LLM classifier.
// ok is true when a rule matched (always HardDeny today).
func PreCheck(req Request) (Verdict, bool) {
	blob := strings.ToLower(strings.Join([]string{
		req.Name, req.Title, req.Kind, req.Args,
	}, "\n"))
	if blob == "" {
		return Verdict{}, false
	}

	if host, ok := findMetadataHost(blob); ok {
		return Verdict{
			Decision: HardDeny,
			Rule:     "Cloud Metadata",
			Reason:   "blocked access to cloud instance metadata (" + host + ")",
		}, true
	}
	if host, ok := findControlPlaneLoopback(blob); ok {
		return Verdict{
			Decision: HardDeny,
			Rule:     "Control Plane",
			Reason:   "blocked access to the Roundpen control plane on loopback (" + host + ")",
		}, true
	}
	if pipeToShell.MatchString(blob) {
		return Verdict{
			Decision: HardDeny,
			Rule:     "Remote Shell Pipe",
			Reason:   "piping remote content directly into a shell is never auto-approved",
		}, true
	}
	if looksLikeSecretExfil(blob) {
		return Verdict{
			Decision: HardDeny,
			Rule:     "Secret Exfiltration",
			Reason:   "reading credentials and sending them outside the workspace is blocked",
		}, true
	}
	return Verdict{}, false
}

var (
	pipeToShell = regexp.MustCompile(
		`(?i)(curl|wget|fetch)\b[^|\n]{0,200}\|\s*(ba)?sh\b` +
			`|` +
			`(?i)\|\s*(ba)?sh\s+-[cs]\b`,
	)
	secretPath = regexp.MustCompile(
		`(?i)(\.env\b|/\.ssh/|id_rsa\b|id_ed25519\b|\.aws/credentials\b|/\.netrc\b|kube/config\b)`,
	)
	exfilSink = regexp.MustCompile(
		`(?i)\b(curl|wget)\b|https?://|pastebin\.|gist\.github`,
	)
	metadataHosts = []string{
		"169.254.169.254",
		"metadata.google.internal",
		"metadata.goog",
	}
	// Roundpen control-plane listen ports commonly used in compose / local dev.
	controlPlanePorts = map[string]bool{
		"9527":  true,
		"19000": true,
		"19001": true,
	}
)

func findMetadataHost(blob string) (string, bool) {
	for _, h := range metadataHosts {
		if strings.Contains(blob, h) {
			return h, true
		}
	}
	return "", false
}

func findControlPlaneLoopback(blob string) (string, bool) {
	// Explicit Roundpen API paths on loopback.
	if (strings.Contains(blob, "127.0.0.1") || strings.Contains(blob, "localhost") ||
		strings.Contains(blob, "[::1]") || strings.Contains(blob, "::1")) &&
		(strings.Contains(blob, "/v1/") || strings.Contains(blob, "/llmgw") ||
			strings.Contains(blob, "/api/")) {
		return "loopback-api", true
	}
	for _, raw := range extractURLs(blob) {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if !isLoopbackHost(host) {
			continue
		}
		port := u.Port()
		if port == "" {
			switch strings.ToLower(u.Scheme) {
			case "https":
				port = "443"
			default:
				port = "80"
			}
		}
		if controlPlanePorts[port] {
			return host + ":" + port, true
		}
		path := strings.ToLower(u.Path)
		if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/llmgw") {
			return host + path, true
		}
	}
	return "", false
}

func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

var urlRe = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

func extractURLs(blob string) []string {
	return urlRe.FindAllString(blob, 32)
}

func looksLikeSecretExfil(blob string) bool {
	return secretPath.MatchString(blob) && exfilSink.MatchString(blob)
}
