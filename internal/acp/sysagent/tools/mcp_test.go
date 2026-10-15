package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/mcp"
)

type stubMCP struct {
	servers []mcp.ServerTools
	res     mcp.CallResult
	err     error
	calls   []string
}

func (s *stubMCP) ToolsForSession(context.Context, string) ([]mcp.ServerTools, error) {
	return s.servers, nil
}

func (s *stubMCP) CallTool(_ context.Context, _, serverID, toolName string, _ json.RawMessage) (mcp.CallResult, error) {
	s.calls = append(s.calls, serverID+"/"+toolName)
	return s.res, s.err
}

func testBinder(api *stubMCP) *tools.MCPBinder {
	return &tools.MCPBinder{API: api, Servers: api.servers}
}

func TestMCPToolSurface(t *testing.T) {
	api := &stubMCP{servers: []mcp.ServerTools{{
		ServerID:    "notion",
		ServerName:  "Notion",
		Description: "Notion workspace.",
		Tools: []mcp.Tool{
			{Name: "search", Description: "Search pages.", InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
			{Name: "post-page", Description: ""},
			{Name: "with spaces + symbols!", Description: "odd"},
		},
	}}}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))

	for _, name := range []string{"mcp_notion_search", "mcp_notion_post_page", "mcp_notion_with_spaces_symbols"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing tool %q", name)
		}
		if !tool.Mutating {
			t.Fatalf("%s must be Mutating (server annotations are never trusted)", name)
		}
		if !strings.HasPrefix(tool.Description, "[MCP: Notion]") {
			t.Fatalf("%s description = %q", name, tool.Description)
		}
	}

	search, _ := reg.Get("mcp_notion_search")
	if search.Parameters == nil || search.Parameters["type"] != "object" {
		t.Fatalf("search parameters = %+v", search.Parameters)
	}
	postPage, _ := reg.Get("mcp_notion_post_page")
	// Empty description falls back to the server description.
	if !strings.Contains(postPage.Description, "Notion workspace.") {
		t.Fatalf("post_page description = %q", postPage.Description)
	}
	// No schema → default empty-object parameters at serialization time.
	if postPage.Parameters != nil {
		t.Fatalf("post_page parameters = %+v", postPage.Parameters)
	}
}

func TestMCPToolNameBudgets(t *testing.T) {
	long := strings.Repeat("very-long-tool-name-", 10)
	api := &stubMCP{servers: []mcp.ServerTools{{
		ServerID: "GitHub.Com", ServerName: "GitHub",
		Tools: []mcp.Tool{
			{Name: long},
			{Name: long + "-copy"}, // sanitizes to the same prefix but differs in tail
		},
	}}}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))
	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("tools = %d", len(list))
	}
	for _, tool := range list {
		if len(tool.Name) > 64 {
			t.Fatalf("tool name too long (%d): %q", len(tool.Name), tool.Name)
		}
		if !strings.HasPrefix(tool.Name, "mcp_github_com_") {
			t.Fatalf("tool name = %q", tool.Name)
		}
	}
	if list[0].Name == list[1].Name {
		t.Fatalf("names must be unique: %q", list[0].Name)
	}
}

func TestMCPToolNameCollisionSuffix(t *testing.T) {
	api := &stubMCP{servers: []mcp.ServerTools{{
		ServerID: "s", ServerName: "S",
		Tools: []mcp.Tool{
			{Name: "a b"},
			{Name: "a-b"},
			{Name: "a_b"},
		},
	}}}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))
	names := map[string]bool{}
	for _, tool := range reg.List() {
		if names[tool.Name] {
			t.Fatalf("duplicate %q", tool.Name)
		}
		names[tool.Name] = true
	}
	if !names["mcp_s_a_b"] || !names["mcp_s_a_b_2"] || !names["mcp_s_a_b_3"] {
		t.Fatalf("names = %v", names)
	}
}

func TestMCPToolCallSuccess(t *testing.T) {
	api := &stubMCP{
		servers: []mcp.ServerTools{{
			ServerID: "exa", ServerName: "Exa",
			Tools: []mcp.Tool{{Name: "web_search"}},
		}},
		res: mcp.CallResult{Text: "results"},
	}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice", Role: "user"},
		"mcp_exa_web_search", json.RawMessage(`{"q":"x"}`))
	if err != nil || out != "results" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if len(api.calls) != 1 || api.calls[0] != "exa/web_search" {
		t.Fatalf("calls = %v (原始工具名必须透传)", api.calls)
	}
}

func TestMCPToolCallToolLevelError(t *testing.T) {
	api := &stubMCP{
		servers: []mcp.ServerTools{{
			ServerID: "exa", ServerName: "Exa",
			Tools: []mcp.Tool{{Name: "web_search"}},
		}},
		res: mcp.CallResult{Text: "quota exceeded", IsError: true},
	}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "mcp_exa_web_search", nil)
	if err != nil {
		t.Fatalf("tool-level errors must be content, not a call error: %v", err)
	}
	if !strings.Contains(out, "quota exceeded") || !strings.Contains(out, "MCP tool error") {
		t.Fatalf("out = %q", out)
	}
}

func TestMCPToolCallTruncates(t *testing.T) {
	api := &stubMCP{
		servers: []mcp.ServerTools{{
			ServerID: "exa", ServerName: "Exa",
			Tools: []mcp.Tool{{Name: "web_search"}},
		}},
		res: mcp.CallResult{Text: strings.Repeat("x", 64<<10)},
	}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "mcp_exa_web_search", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 32<<10+80 || !strings.Contains(out, "truncated") {
		t.Fatalf("len = %d", len(out))
	}
}

func TestMCPToolCallErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"needs reauth", mcp.ErrNeedsReauth, "Settings → MCP"},
		{"not connected", mcp.ErrNotConnected, "Settings → MCP"},
		{"network", &mcp.MCPError{Class: mcp.ClassNetwork, Msg: "down"}, "unreachable"},
		{"rate limit", &mcp.MCPError{Class: mcp.ClassRateLimit, RetryAfter: 12 * time.Second, Msg: "slow"}, "retry after 12s"},
		{"auth", &mcp.MCPError{Class: mcp.ClassAuth, Msg: "expired"}, "Settings → MCP"},
	}
	for _, tc := range cases {
		api := &stubMCP{
			servers: []mcp.ServerTools{{
				ServerID: "exa", ServerName: "Exa",
				Tools: []mcp.Tool{{Name: "web_search"}},
			}},
			err: tc.err,
		}
		reg := tools.NewRegistry()
		tools.RegisterMCP(reg, testBinder(api))
		_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "mcp_exa_web_search", nil)
		if err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Fatalf("%s: err = %q, want %q", tc.name, err.Error(), tc.wantSub)
		}
	}
}

func TestMCPRoutineSurfaceExcludesMCP(t *testing.T) {
	api := &stubMCP{servers: []mcp.ServerTools{{
		ServerID: "exa", ServerName: "Exa",
		Tools: []mcp.Tool{{Name: "web_search"}},
	}}}

	chat := tools.NewRegistry()
	tools.RegisterSession(chat, tools.SessionSurface{MCP: testBinder(api)})
	if _, ok := chat.Get("mcp_exa_web_search"); !ok {
		t.Fatal("interactive session missing MCP tool")
	}

	routine := tools.NewRegistry()
	tools.RegisterSession(routine, tools.SessionSurface{MCP: testBinder(api), RunKey: "RUN-1", Autonomy: "read"})
	if _, ok := routine.Get("mcp_exa_web_search"); ok {
		t.Fatal("scheduled runs must not get MCP tools")
	}
}

func TestMCPBinderWithoutAPI(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, &tools.MCPBinder{})
	tools.RegisterMCP(reg, nil)
	if len(reg.List()) != 0 {
		t.Fatal("binder without API must register nothing")
	}
}

func TestMCPUnclassifiedErrorSurfaced(t *testing.T) {
	api := &stubMCP{
		servers: []mcp.ServerTools{{
			ServerID: "exa", ServerName: "Exa",
			Tools: []mcp.Tool{{Name: "web_search"}},
		}},
		err: errors.New("weird protocol problem"),
	}
	reg := tools.NewRegistry()
	tools.RegisterMCP(reg, testBinder(api))
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "mcp_exa_web_search", nil)
	if err == nil || !strings.Contains(err.Error(), "weird protocol problem") {
		t.Fatalf("err = %v", err)
	}
}
