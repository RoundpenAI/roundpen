package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// Client talks to a download backend.
type Client interface {
	Submit(ctx context.Context, req SubmitRequest) error
	ListTasks(ctx context.Context) ([]Task, error)
}

// SubmitRequest is one download job.
type SubmitRequest struct {
	URL      string
	SavePath string
	Category string
}

// Task is a progress summary.
type Task struct {
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	SavePath string  `json:"savePath,omitempty"`
	Size     int64   `json:"size,omitempty"`
}

// NewClient builds a Client for the backend type.
func NewClient(b *Backend) (Client, error) {
	if b == nil {
		return nil, fmt.Errorf("backend required")
	}
	switch b.Type {
	case TypeQBittorrent, "":
		return &qbClient{b: b, http: &http.Client{Timeout: 30 * time.Second}}, nil
	case TypeWebhook:
		return &webhookClient{b: b, http: &http.Client{Timeout: 30 * time.Second}}, nil
	default:
		return nil, fmt.Errorf("backend type %q is not implemented yet", b.Type)
	}
}

type qbClient struct {
	b    *Backend
	http *http.Client
}

func (c *qbClient) session(ctx context.Context) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	cli := &http.Client{Timeout: c.http.Timeout, Jar: jar}
	form := url.Values{}
	form.Set("username", c.b.Username)
	form.Set("password", c.b.Password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.b.BaseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("qBittorrent login HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	if strings.Contains(strings.ToLower(string(body)), "fails") {
		return nil, fmt.Errorf("qBittorrent login failed")
	}
	return cli, nil
}

func (c *qbClient) Submit(ctx context.Context, in SubmitRequest) error {
	cli, err := c.session(ctx)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("urls", strings.TrimSpace(in.URL))
	if p := strings.TrimSpace(in.SavePath); p != "" {
		form.Set("savepath", p)
	}
	if cat := strings.TrimSpace(in.Category); cat != "" {
		form.Set("category", cat)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.b.BaseURL+"/api/v2/torrents/add", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 300 {
		return fmt.Errorf("qBittorrent add HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *qbClient) ListTasks(ctx context.Context) ([]Task, error) {
	cli, err := c.session(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.b.BaseURL+"/api/v2/torrents/info", nil)
	if err != nil {
		return nil, err
	}
	res, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("qBittorrent info HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw []struct {
		Name     string  `json:"name"`
		State    string  `json:"state"`
		Progress float64 `json:"progress"`
		SavePath string  `json:"save_path"`
		Size     int64   `json:"size"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(raw))
	for _, t := range raw {
		out = append(out, Task{
			Name: t.Name, State: t.State, Progress: t.Progress,
			SavePath: t.SavePath, Size: t.Size,
		})
	}
	return out, nil
}

type webhookClient struct {
	b    *Backend
	http *http.Client
}

func (c *webhookClient) Submit(ctx context.Context, in SubmitRequest) error {
	payload, _ := json.Marshal(map[string]string{
		"url": in.URL, "savePath": in.SavePath, "category": in.Category,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.b.BaseURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.b.Password != "" {
		req.Header.Set("Authorization", "Bearer "+c.b.Password)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("webhook HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *webhookClient) ListTasks(context.Context) ([]Task, error) {
	return nil, fmt.Errorf("webhook backends do not support ListDownloadTasks")
}
