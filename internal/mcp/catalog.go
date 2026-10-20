// Package mcp implements the Model Context Protocol connector layer: a
// curated server catalog, per-user connections (API key or OAuth), and the
// client the System Agent uses to call remote MCP tools.
//
// The catalog doubles as the direct-connect allowlist: users may only connect
// servers listed here (see docs/mcp-catalog.md for the compatibility baseline).
package mcp

import "strings"

// AuthMode describes how users authenticate to a catalog server.
type AuthMode string

const (
	// AuthOAuth means the server only accepts OAuth (discovery + DCR).
	AuthOAuth AuthMode = "oauth"
	// AuthAPIKey means the server only accepts a personal API key / token.
	AuthAPIKey AuthMode = "api_key"
	// AuthBoth means the server accepts OAuth or an API key.
	AuthBoth AuthMode = "both"
)

// Server is one allowlisted remote MCP server.
type Server struct {
	ID   string
	Name string
	// Endpoint is the full Streamable HTTP MCP URL.
	Endpoint string
	Auth     AuthMode
	// Group buckets servers for the catalog UI (knowledge, tasks, collab,
	// dev, business, design, search).
	Group string
	// Description is a one-line summary shown in the catalog and in tool
	// descriptions.
	Description string
	DocsURL     string
}

// MCP servers from docs/mcp-catalog.md (P0). Endpoints verified 2026-10;
// official docs are the source of truth. GitHub / GitLab are omitted: Connected
// apps already cover git / gh / glab via OAuth or personal access tokens.
var servers = []Server{
	{
		ID: "notion", Name: "Notion", Endpoint: "https://mcp.notion.com",
		Auth: AuthOAuth, Group: "knowledge",
		Description: "Search and read pages, databases and comments in your Notion workspace.",
		DocsURL:     "https://developers.notion.com/docs/mcp",
	},
	{
		ID: "linear", Name: "Linear", Endpoint: "https://mcp.linear.app/mcp",
		Auth: AuthBoth, Group: "tasks",
		Description: "Work with Linear issues, projects, cycles and comments.",
		DocsURL:     "https://linear.app/docs/mcp",
	},
	{
		ID: "asana", Name: "Asana", Endpoint: "https://mcp.asana.com/v2/mcp",
		Auth: AuthOAuth, Group: "tasks",
		Description: "Manage Asana projects, tasks, and goals.",
		DocsURL:     "https://developers.asana.com/docs/integrating-with-asanas-mcp-server",
	},
	{
		ID: "atlassian", Name: "Atlassian Rovo", Endpoint: "https://mcp.atlassian.com/v2/mcp",
		Auth: AuthOAuth, Group: "collab",
		Description: "Search and update Jira issues, Confluence pages and Bitbucket in one server.",
		DocsURL:     "https://support.atlassian.com/atlassian-rovo-mcp-server/docs/getting-started-with-the-atlassian-remote-mcp-server/",
	},
	{
		ID: "sentry", Name: "Sentry", Endpoint: "https://mcp.sentry.dev",
		Auth: AuthOAuth, Group: "dev",
		Description: "Investigate errors, issues and releases from Sentry.",
		DocsURL:     "https://docs.sentry.io/product/sentry-mcp/",
	},
	{
		ID: "stripe", Name: "Stripe", Endpoint: "https://mcp.stripe.com",
		Auth: AuthBoth, Group: "business",
		Description: "Query payments, customers and invoices. Money-moving writes need approval.",
		DocsURL:     "https://docs.stripe.com/mcp",
	},
	{
		ID: "hubspot", Name: "HubSpot", Endpoint: "https://mcp.hubspot.com",
		Auth: AuthOAuth, Group: "business",
		Description: "CRM contacts, companies and deals in HubSpot.",
		DocsURL:     "https://developers.hubspot.com/ai-tools/mcp",
	},
	{
		ID: "figma", Name: "Figma", Endpoint: "https://mcp.figma.com/mcp",
		Auth: AuthOAuth, Group: "design",
		Description: "Inspect designs, components and variables. Requires a paid Figma plan.",
		DocsURL:     "https://help.figma.com/hc/en-us/articles/32132100833559-Guide-to-the-Dev-Mode-MCP-Server",
	},
	{
		ID: "zoom", Name: "Zoom", Endpoint: "https://mcp.zoom.us",
		Auth: AuthOAuth, Group: "collab",
		Description: "Meetings, recordings and account data from Zoom.",
		DocsURL:     "https://developers.zoom.us/docs/mcp/",
	},
	{
		ID: "supabase", Name: "Supabase", Endpoint: "https://mcp.supabase.com/mcp",
		Auth: AuthOAuth, Group: "dev",
		Description: "Projects, tables and SQL against your Supabase databases.",
		DocsURL:     "https://supabase.com/docs/guides/getting-started/mcp",
	},
	{
		ID: "exa", Name: "Exa", Endpoint: "https://mcp.exa.ai/mcp",
		Auth: AuthAPIKey, Group: "search",
		Description: "Neural web search and page retrieval via Exa.",
		DocsURL:     "https://docs.exa.ai/reference/exa-mcp",
	},
	{
		ID: "todoist", Name: "Todoist", Endpoint: "https://ai.todoist.net/mcp",
		Auth: AuthOAuth, Group: "tasks",
		Description: "Tasks, projects and labels in Todoist.",
		DocsURL:     "https://github.com/Doist/todoist-mcp",
	},
}

var serversByID = func() map[string]Server {
	m := make(map[string]Server, len(servers))
	for _, s := range servers {
		m[s.ID] = s
	}
	return m
}()

// Catalog returns the allowlisted servers in display order.
func Catalog() []Server {
	out := make([]Server, len(servers))
	copy(out, servers)
	return out
}

// Lookup finds an allowlisted server by id (case-insensitive).
func Lookup(id string) (Server, bool) {
	s, ok := serversByID[strings.ToLower(strings.TrimSpace(id))]
	return s, ok
}
