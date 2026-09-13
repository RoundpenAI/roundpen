// internal/browser/cdp.go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// candidateWSPaths are tried in order when the endpoint has no explicit path.
// browserless v2: /chrome for the chrome image, /chromium for chromium, / for
// single-browser images. /json/version cannot be used for discovery: it returns
// the server's bind address (ws://0.0.0.0:3000/).
var candidateWSPaths = []string{"/chrome", "/chromium", "/"}

func candidatePaths(endpointPath string) []string {
	p := strings.TrimSpace(endpointPath)
	if p == "" || p == "/" {
		return append([]string(nil), candidateWSPaths...)
	}
	return []string{p}
}

func buildWSURL(endpoint, wsPath, token string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("cdp endpoint %q: %w", endpoint, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("cdp endpoint %q: scheme must be http(s) or ws(s)", endpoint)
	}
	if u.Host == "" {
		return "", fmt.Errorf("cdp endpoint %q: host is required", endpoint)
	}
	if !strings.HasPrefix(wsPath, "/") {
		wsPath = "/" + wsPath
	}
	u.Path = wsPath
	u.Fragment = ""
	if token = strings.TrimSpace(token); token != "" {
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// ProbeResult describes a reachable browserless endpoint.
type ProbeResult struct {
	Endpoint   string   `json:"endpoint"`
	Path       string   `json:"path"`
	Version    string   `json:"version,omitempty"`
	Playwright []string `json:"playwright,omitempty"`
	Puppeteer  []string `json:"puppeteer,omitempty"`
}

// ProbeCDP checks connectivity: GET {origin}/meta for the browserless version,
// then a CDP websocket handshake across the candidate paths.
func ProbeCDP(ctx context.Context, endpoint, token string) (*ProbeResult, error) {
	res := &ProbeResult{Endpoint: strings.TrimSpace(endpoint)}
	u, err := url.Parse(res.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("cdp endpoint %q: invalid URL", endpoint)
	}
	meta := *u
	switch strings.ToLower(meta.Scheme) {
	case "ws":
		meta.Scheme = "http"
	case "wss":
		meta.Scheme = "https"
	}
	meta.Path = "/meta"
	meta.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.String(), nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	if resp, err := hc.Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			var metaResp struct {
				Version    string   `json:"version"`
				Playwright []string `json:"playwright"`
				Puppeteer  []string `json:"puppeteer"`
			}
			if json.Unmarshal(body, &metaResp) == nil {
				res.Version = metaResp.Version
				res.Playwright = metaResp.Playwright
				res.Puppeteer = metaResp.Puppeteer
			}
		}
	}
	path, err := ProbeWSPath(ctx, endpoint, token)
	if err != nil {
		return res, err
	}
	res.Path = path
	return res, nil
}

// ProbeWSPath returns the first candidate path whose websocket handshake succeeds.
func ProbeWSPath(ctx context.Context, endpoint, token string) (string, error) {
	var lastErr error
	for _, p := range candidatePaths(endpointPathOf(endpoint)) {
		wsURL, err := buildWSURL(endpoint, p, token)
		if err != nil {
			return "", err
		}
		if err := wsHandshake(ctx, wsURL); err != nil {
			lastErr = err
			continue
		}
		return p, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate websocket path")
	}
	return "", fmt.Errorf("cdp websocket probe %s: %w", endpoint, lastErr)
}

func endpointPathOf(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	return u.Path
}

func wsHandshake(ctx context.Context, wsURL string) error {
	u, err := url.Parse(wsURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	hc := &http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("%s: HTTP %d", u.Path, resp.StatusCode)
	}
	return nil
}
