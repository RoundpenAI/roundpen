// Package fnosfile calls the fnOS open API over its Unix socket.
// The token is read from the environment on every call and is never stored.
package fnosfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultSocket = "/var/run/trim_open_gateway_apiscope.socket"

// Client talks to POST /api/v1/trimapp.
type Client struct {
	AppName string
	Socket  string
	// Token returns the current TRIM_API_TOKEN. Nil reads the environment.
	Token func() string
	// HTTP overrides the Unix-socket client (tests).
	HTTP *http.Client
	// BaseURL overrides the request URL (tests). Empty uses http://localhost.
	BaseURL string
}

// FromEnv returns a client that is inert until TRIM_API_TOKEN is set.
func FromEnv() *Client {
	name := strings.TrimSpace(os.Getenv("TRIM_APPNAME"))
	if name == "" {
		name = "roundpen"
	}
	return &Client{AppName: name, Socket: defaultSocket}
}

// Enabled reports whether a token is currently available.
func (c *Client) Enabled() bool {
	return c != nil && c.token() != ""
}

func (c *Client) token() string {
	if c == nil {
		return ""
	}
	if c.Token != nil {
		return strings.TrimSpace(c.Token())
	}
	return strings.TrimSpace(os.Getenv("TRIM_API_TOKEN"))
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) call(ctx context.Context, req string, data any, dest any) error {
	if !c.Enabled() {
		return fmt.Errorf("fnos api token is not set")
	}
	body, err := json.Marshal(map[string]any{
		"reqId":   fmt.Sprintf("%d", time.Now().UnixNano()),
		"req":     req,
		"appName": c.AppName,
		"data":    data,
	})
	if err != nil {
		return err
	}
	url := c.BaseURL
	if url == "" {
		url = "http://localhost/api/v1/trimapp"
	} else {
		url = strings.TrimRight(url, "/") + "/api/v1/trimapp"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token())
	resp, err := c.http().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fnos api %s: http %d", req, resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("fnos api %s: %w", req, err)
	}
	if env.Code != 0 {
		msg := strings.TrimSpace(env.Msg)
		if msg == "" {
			msg = fmt.Sprintf("code %d", env.Code)
		}
		return fmt.Errorf("fnos api %s: %s", req, msg)
	}
	if dest == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, dest); err != nil {
		return fmt.Errorf("fnos api %s: %w", req, err)
	}
	return nil
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	socket := c.Socket
	if socket == "" {
		socket = defaultSocket
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
}

// ACL is one path's permissions for a fnOS user.
type ACL struct {
	Path      string `json:"path"`
	Readable  bool   `json:"readable"`
	Writable  bool   `json:"writable"`
	Deletable bool   `json:"deletable"`
}

// CheckACL asks whether uid may read and write path.
func (c *Client) CheckACL(ctx context.Context, uid int, path string) (ACL, error) {
	var rows []ACL
	err := c.call(ctx, "trim.file.checkUserACL", map[string]any{
		"uid":  uid,
		"path": path,
	}, &rows)
	if err != nil {
		return ACL{}, err
	}
	for _, row := range rows {
		if row.Path == path {
			return row, nil
		}
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return ACL{Path: path}, nil
}

// UserFolders lists directories uid has granted to this app.
func (c *Client) UserFolders(ctx context.Context, uid int) ([]string, error) {
	var data struct {
		Paths []string `json:"paths"`
	}
	if err := c.call(ctx, "trim.file.getUserAccessibleFolders", map[string]any{"uid": uid}, &data); err != nil {
		return nil, err
	}
	return data.Paths, nil
}

// DisplayPath converts an internal path to a label safe to show in the UI.
func (c *Client) DisplayPath(ctx context.Context, path, language string) (string, error) {
	if language == "" {
		language = "zh-CN"
	}
	var data struct {
		Result []struct {
			Path         string `json:"path"`
			SemanticPath string `json:"semanticPath"`
		} `json:"result"`
	}
	err := c.call(ctx, "trim.file.convertPath", map[string]any{
		"path":     []string{path},
		"language": language,
	}, &data)
	if err != nil {
		return "", err
	}
	for _, row := range data.Result {
		if strings.TrimSpace(row.SemanticPath) != "" {
			return row.SemanticPath, nil
		}
	}
	return "", fmt.Errorf("fnos api convertPath: empty")
}
