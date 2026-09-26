package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// RegisterPreviewDomain adds ClaimPreviewDomain: book a public address for one
// port of the Agent workspace, first come first served. zone is the wildcard
// the deployment serves previews from (ROUNDPEN_PREVIEW_DOMAIN); without one
// there is nothing to hand out, so the tool is not registered.
//
// The claim itself goes through the control-plane API (like issues), so name
// validation and ownership live in one place — the HTTP layer.
func RegisterPreviewDomain(r *Registry, binder *AgentBinder, api *RoundpenHTTP, zone string) {
	zone = strings.TrimSpace(zone)
	if r == nil || binder == nil || api == nil || zone == "" {
		return
	}
	r.Register(Tool{
		Name: "ClaimPreviewDomain",
		Description: "Book a public address under " + zone + " for an HTTP port of the Agent workspace, " +
			"e.g. name \"demo\" serves the app on demo." + zone + ", and return the ready-to-open link. " +
			"Names are first come, first served and stay with the user until released; claiming the same name " +
			"again moves it to the port given now, so a rebuilt workspace keeps its address. " +
			"Use it when the user should open or share something running on a port — the link carries a " +
			"short-lived token, so mint a fresh one by calling this again rather than reusing an old link.",
		Parameters: objectSchema(map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Address label to book: a-z, 0-9 and inner hyphens, single label (no dots)",
			},
			"port": map[string]any{
				"type":        "integer",
				"description": "Port the app listens on inside the Agent workspace (1-65535)",
			},
		}, "name", "port"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Name string `json:"name"`
				Port int    `json:"port"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", err
			}
			if strings.TrimSpace(in.Name) == "" || in.Port <= 0 || in.Port > 65535 {
				return "", fmt.Errorf("name and port are required")
			}
			id, err := binder.ensureID(ctx, actor)
			if err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/preview-domains", map[string]any{
				"name":      strings.TrimSpace(in.Name),
				"sandboxID": id,
				"port":      in.Port,
			})
		},
	})
}
