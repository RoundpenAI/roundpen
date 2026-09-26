package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RoundpenHTTP calls Roundpen control-plane APIs as the actor (X-API-Key).
// Tools whose validation and ownership live in the HTTP layer (issues) use it
// instead of binding control-plane services directly.
type RoundpenHTTP struct {
	BaseURL    string // e.g. http://127.0.0.1:19001
	HTTPClient *http.Client
}

func (c *RoundpenHTTP) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *RoundpenHTTP) do(ctx context.Context, actor Actor, method, path string, body any) (string, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return "", err
	}
	if actor.APIKey != "" {
		req.Header.Set("X-API-Key", actor.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(raw) == 0 {
		return fmt.Sprintf(`{"ok":true,"status":%d}`, resp.StatusCode), nil
	}
	return string(raw), nil
}
