package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/mcp"
)

const (
	// maxMCPResult caps the text handed back to the model per tool call.
	maxMCPResult = 32 << 10
	// maxToolName is the function-name budget most providers enforce.
	maxToolName = 64
)

// MCPAPI is the subset of mcp.Service the MCP tools need.
type MCPAPI interface {
	ToolsForSession(ctx context.Context, userID string) ([]mcp.ServerTools, error)
	CallTool(ctx context.Context, userID, serverID, toolName string, args json.RawMessage) (mcp.CallResult, error)
}

// MCPBinder exposes tools of the user's connected MCP servers. Servers is the
// snapshot taken when the session started.
type MCPBinder struct {
	API     MCPAPI
	Servers []mcp.ServerTools
}

// RegisterMCP adds one tool per connected-server tool. Every MCP tool is
// Mutating: the server's own annotations (readOnlyHint) are never trusted for
// gating, so each tool's first call per session goes through approval.
func RegisterMCP(r *Registry, b *MCPBinder) {
	if r == nil || b == nil || b.API == nil {
		return
	}
	used := map[string]bool{}
	for _, server := range b.Servers {
		for _, tool := range server.Tools {
			description := strings.TrimSpace(tool.Description)
			if description == "" {
				description = strings.TrimSpace(server.Description)
			}
			r.Register(Tool{
				Name:        mcpToolName(server.ServerID, tool.Name, used),
				Description: fmt.Sprintf("[MCP: %s] %s", server.ServerName, description),
				Parameters:  toolParameters(tool.InputSchema),
				Mutating:    true,
				Call:        b.caller(server, tool.Name),
			})
		}
	}
}

func (b *MCPBinder) caller(server mcp.ServerTools, toolName string) func(context.Context, Actor, json.RawMessage) (string, error) {
	return func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
		start := time.Now()
		res, err := b.API.CallTool(ctx, actor.Username, server.ServerID, toolName, args)
		actx := authz.WithActor(ctx, actor.Authz())
		if err != nil {
			audit.Record(actx, "mcp.tool.call", "server", server.ServerID, "tool", toolName,
				"ok", false, "err", truncateRunes(err.Error(), 200),
				"duration_ms", time.Since(start).Milliseconds())
			return "", mapMCPToolError(server.ServerName, err)
		}
		audit.Record(actx, "mcp.tool.call", "server", server.ServerID, "tool", toolName,
			"ok", !res.IsError, "duration_ms", time.Since(start).Milliseconds())
		text := res.Text
		if len(text) > maxMCPResult {
			text = truncateRunes(text, maxMCPResult) + "\n\n[Content truncated due to length]"
		}
		if res.IsError {
			if strings.TrimSpace(text) == "" {
				text = "the tool reported an error with no output"
			}
			return "MCP tool error: " + text, nil
		}
		if strings.TrimSpace(text) == "" {
			return "(empty result)", nil
		}
		return text, nil
	}
}

// mapMCPToolError turns transport and auth failures into readable guidance the
// model can relay; tool-level failures never reach this path.
func mapMCPToolError(serverName string, err error) error {
	switch {
	case errors.Is(err, mcp.ErrNeedsReauth), errors.Is(err, mcp.ErrNotConnected):
		return fmt.Errorf("The %s connection is not usable right now — tell the user to reconnect it under Settings → MCP.", serverName)
	case errors.Is(err, mcp.ErrServerUnknown):
		return fmt.Errorf("MCP server %s is no longer in the catalog; its connection should be removed under Settings → MCP.", serverName)
	}
	var me *mcp.MCPError
	if errors.As(err, &me) {
		switch me.Class {
		case mcp.ClassNetwork:
			return fmt.Errorf("MCP server %s is unreachable right now; the session can continue. Retry later.", serverName)
		case mcp.ClassRateLimit:
			if me.RetryAfter > 0 {
				return fmt.Errorf("%s rate limited this call; retry after %ds.", serverName, int(me.RetryAfter.Round(time.Second).Seconds()))
			}
			return fmt.Errorf("%s rate limited this call; retry shortly.", serverName)
		case mcp.ClassAuth:
			return fmt.Errorf("The %s connection expired — tell the user to reconnect it under Settings → MCP.", serverName)
		}
	}
	return fmt.Errorf("MCP call to %s failed: %s", serverName, truncateRunes(err.Error(), 300))
}

var toolNameSanitizer = regexp.MustCompile(`[^a-z0-9_]+`)

// sanitizeToolSegment maps arbitrary server/tool names onto [a-z0-9_].
func sanitizeToolSegment(s string) string {
	s = toolNameSanitizer.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "x"
	}
	return s
}

// mcpToolName builds mcp_<server>_<tool>, truncating to the provider budget
// and de-duplicating collisions with a numeric suffix.
func mcpToolName(serverID, toolName string, used map[string]bool) string {
	base := "mcp_" + sanitizeToolSegment(serverID) + "_" + sanitizeToolSegment(toolName)
	if len(base) > maxToolName {
		base = base[:maxToolName]
	}
	name := base
	for n := 2; used[name]; n++ {
		suffix := fmt.Sprintf("_%d", n)
		trimmed := base
		if len(trimmed) > maxToolName-len(suffix) {
			trimmed = trimmed[:maxToolName-len(suffix)]
		}
		name = trimmed + suffix
	}
	used[name] = true
	return name
}

// toolParameters converts a server tool's input schema; non-object schemas are
// dropped so the provider gets the default empty-object parameters.
func toolParameters(schema json.RawMessage) map[string]any {
	if len(schema) == 0 {
		return nil
	}
	var params map[string]any
	if err := json.Unmarshal(schema, &params); err != nil {
		return nil
	}
	if t, _ := params["type"].(string); t != "object" {
		return nil
	}
	return params
}
