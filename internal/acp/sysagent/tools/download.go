package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/download"
	"github.com/RoundpenAI/roundpen/internal/policy"
)

// DownloadBackends lists and drives user download clients.
type DownloadBackends interface {
	List(ctx context.Context, userID string) ([]download.Backend, error)
	Get(ctx context.Context, userID, id string) (*download.Backend, error)
}

// DownloadBinder wires download tools for one session user.
type DownloadBinder struct {
	UserID   string
	Store    DownloadBackends
	Host     HostFiles
	Policy   func(ctx context.Context) *policy.Profile
	NewClient func(*download.Backend) (download.Client, error)
}

// RegisterDownload adds ListDownloadBackends / SubmitDownload / ListDownloadTasks.
func RegisterDownload(r *Registry, b *DownloadBinder) {
	if r == nil || b == nil || b.Store == nil {
		return
	}
	if b.NewClient == nil {
		b.NewClient = download.NewClient
	}
	r.Register(Tool{
		Name: "ListDownloadBackends",
		Description: "List configured download clients (qBittorrent, webhook, …). " +
			"Users add them under Settings → Home NAS assistants.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			if actor.Username != "" && actor.Username != b.UserID {
				return "", fmt.Errorf("download backends denied: wrong user")
			}
			list, err := b.Store.List(ctx, b.UserID)
			if err != nil {
				return "", err
			}
			type view struct {
				ID              string `json:"id"`
				Name            string `json:"name"`
				Type            string `json:"type"`
				DefaultSavePath string `json:"defaultSavePath,omitempty"`
			}
			out := make([]view, 0, len(list))
			for _, x := range list {
				out = append(out, view{ID: x.ID, Name: x.Name, Type: x.Type, DefaultSavePath: x.DefaultSavePath})
			}
			raw, _ := json.Marshal(map[string]any{"backends": out})
			return string(raw), nil
		},
	})
	r.Register(Tool{
		Name: "SubmitDownload",
		Description: "Submit a magnet/HTTP URL to a configured download backend after the user confirms. " +
			"save_path must sit inside a readwrite directory grant when set.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"url":         map[string]any{"type": "string", "description": "Magnet or http(s) torrent/URL"},
			"backend_id":  map[string]any{"type": "string", "description": "Backend id from ListDownloadBackends; default first"},
			"save_path":   map[string]any{"type": "string", "description": "Absolute save directory under a readwrite grant"},
			"category":    map[string]any{"type": "string", "description": "Optional qBittorrent category"},
		}, "url"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				URL       string `json:"url"`
				BackendID string `json:"backend_id"`
				SavePath  string `json:"save_path"`
				Category  string `json:"category"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.URL) == "" {
				return "", fmt.Errorf("url is required")
			}
			return b.submit(ctx, actor, in.URL, in.BackendID, in.SavePath, in.Category)
		},
	})
	r.Register(Tool{
		Name:        "ListDownloadTasks",
		Description: "List torrents/tasks on a download backend (progress summary).",
		Parameters: objectSchema(map[string]any{
			"backend_id": map[string]any{"type": "string", "description": "Backend id; default first"},
		}),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				BackendID string `json:"backend_id"`
			}
			_ = json.Unmarshal(args, &in)
			return b.listTasks(ctx, actor, in.BackendID)
		},
	})
}

func (b *DownloadBinder) resolve(ctx context.Context, id string) (*download.Backend, error) {
	id = strings.TrimSpace(id)
	if id != "" {
		return b.Store.Get(ctx, b.UserID, id)
	}
	list, err := b.Store.List(ctx, b.UserID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no download backends configured — add one in Settings → Home NAS assistants")
	}
	full, err := b.Store.Get(ctx, b.UserID, list[0].ID)
	return full, err
}

func (b *DownloadBinder) submit(ctx context.Context, actor Actor, rawURL, backendID, savePath, category string) (string, error) {
	if actor.Username != "" && actor.Username != b.UserID {
		return "", fmt.Errorf("download denied: wrong user")
	}
	be, err := b.resolve(ctx, backendID)
	if err != nil {
		return "", err
	}
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		savePath = strings.TrimSpace(be.DefaultSavePath)
	}
	if savePath != "" {
		if err := b.checkSavePath(ctx, savePath); err != nil {
			return "", err
		}
	}
	cli, err := b.NewClient(be)
	if err != nil {
		return "", err
	}
	if err := cli.Submit(ctx, download.SubmitRequest{URL: rawURL, SavePath: savePath, Category: category}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Submitted to %s (%s); save_path=%s", be.Name, be.Type, savePath), nil
}

func (b *DownloadBinder) listTasks(ctx context.Context, actor Actor, backendID string) (string, error) {
	if actor.Username != "" && actor.Username != b.UserID {
		return "", fmt.Errorf("download denied: wrong user")
	}
	be, err := b.resolve(ctx, backendID)
	if err != nil {
		return "", err
	}
	cli, err := b.NewClient(be)
	if err != nil {
		return "", err
	}
	tasks, err := cli.ListTasks(ctx)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(map[string]any{"backendId": be.ID, "tasks": tasks})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (b *DownloadBinder) checkSavePath(ctx context.Context, savePath string) error {
	abs, isHost, err := hostAbsPath(savePath)
	if err != nil {
		return err
	}
	if !isHost {
		return fmt.Errorf("save_path must be an absolute host path outside /workspace")
	}
	if b.Policy != nil {
		if p := b.Policy(ctx); p != nil {
			d := policy.CheckDirectory(p, abs, "readwrite")
			if !d.Allowed {
				return fmt.Errorf("save_path denied: %s", d.Reason)
			}
			return nil
		}
	}
	if b.Host == nil {
		return fmt.Errorf("cannot verify save_path grants in this session")
	}
	// Fall back: mkdir authorize via HostMkdir dry — use ListGrants prefix check.
	grants, err := b.Host.ListGrants(ctx, Actor{Username: b.UserID})
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.Mode != "readwrite" {
			continue
		}
		root := strings.TrimSpace(g.Path)
		if abs == root || strings.HasPrefix(abs, root+"/") {
			return nil
		}
	}
	return fmt.Errorf("save_path %s is outside readwrite directory grants", abs)
}
