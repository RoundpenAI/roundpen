# MCP catalog: recommended servers and compatibility baseline

Roundpen's connector strategy: native connectors stay first-class (e.g. Google
Gmail/Calendar with a user-owned OAuth client, read-only tools — see
`docs/connectors.md`); MCP is the extension layer for the long tail of SaaS.
This document is both the recommended MCP server catalog and the direct-connect
allowlist. Servers not listed here require review before they can be connected.
The implementation (connection flows, tool registration, storage) is described
in `docs/mcp-connectors.md`; the P0 table below is mirrored in
`internal/mcp/catalog.go`, which is the runtime source of truth.

Tiers:

- **P0 — primary compatibility targets.** Vendor-official, remote-hosted,
  per-user OAuth or per-user API key. Each gets end-to-end verification and
  regression coverage before it ships in the catalog UI.
- **P1 — follow-up.** Usable, but needs an auth/hosting adapter (bot tokens,
  Entra tenants, local stdio) or is in preview.
- **P2 — fallback.** Aggregation gateways for the long tail; they add a third
  party to the trust path, so they never carry P0-level compatibility promises.

## P0 — primary compatibility (12)

Coverage rationale: personal-agent high-frequency scenarios (notes, tasks,
collaboration, dev, payments, CRM, design, comms, data, search) and all three
auth archetypes (OAuth-only, OAuth + API key, key-only).

| # | Server | Endpoint | Auth | Adapter notes |
|---|--------|----------|------|---------------|
| 1 | Notion | `https://mcp.notion.com` | OAuth | Knowledge base; read-heavy |
| 2 | Linear | `https://mcp.linear.app/mcp` | OAuth 2.1 or API key | Exercises dual-path auth |
| 3 | Asana ([docs](https://developers.asana.com/docs/integrating-with-asanas-mcp-server)) | `https://mcp.asana.com/v2/mcp` | OAuth | |
| 4 | Atlassian Rovo ([docs](https://support.atlassian.com/atlassian-rovo-mcp-server/docs/getting-started-with-the-atlassian-remote-mcp-server/)) | `https://mcp.atlassian.com/v2/mcp` | OAuth 2.1 | One server covers Jira / Confluence / Bitbucket |
| 5 | Sentry | `https://mcp.sentry.dev` | OAuth | |
| 6 | Stripe ([docs](https://docs.stripe.com/mcp)) | `https://mcp.stripe.com` | OAuth or Agent API keys | Money-moving writes → approval gate is mandatory |
| 7 | HubSpot ([docs](https://developers.hubspot.com/ai-tools/mcp)) | `https://mcp.hubspot.com` | OAuth 2.0 PKCE | PKCE flow |
| 8 | Figma | `https://mcp.figma.com/mcp` | OAuth 2.0 | Paid plan required; design reads. Endpoint from third-party sources — re-verify against Figma docs |
| 9 | Zoom ([docs](https://developers.zoom.us/docs/mcp/)) | `https://mcp.zoom.us` | OAuth | |
| 10 | Supabase | `https://mcp.supabase.com/mcp` | OAuth 2.1 | SQL write tools → approval gate |
| 11 | Exa | `https://mcp.exa.ai/mcp` | API key | Non-OAuth path; host must custody the key |
| 12 | Todoist ([repo](https://github.com/Doist/todoist-mcp)) | `https://ai.todoist.net/mcp` | OAuth | |

Endpoints verified 2026-10; treat official docs as source of truth at
implementation time.

Deliberately out of P0:
- GitHub remote MCP — overlaps Connected apps GitHub account linking (git / gh);
  keep only the OAuth connector in the apps list.
- GitLab remote MCP — Connected apps host + PAT injects `GITLAB_TOKEN` for
  `git` / `glab`; no separate MCP connector.
- Search/scrape servers that overlap the existing `WebSearch` / `WebFetch` /
  browser tools (Tavily, Brave, Firecrawl) — revisit on demand.

## Compatibility checklist (required for every P0 server)

1. **Protocol revision.** Record which revision the server serves (2025-11-25
   or stateless 2026-07-28). The client must speak both during the ecosystem
   transition; newest SDKs are 2.x, SSE transport is deprecated — use
   Streamable HTTP only.
2. **Auth discovery.** Probe `WWW-Authenticate` / RFC 9728 protected resource
   metadata. Record whether the server supports DCR or needs a pre-registered
   client — the latter reuses the existing BYO client form pattern from Google
   connectors.
3. **Credential lifecycle.** Per-user token storage (reuse the grant table
   shape, `kind` like `mcp:<server>`), refresh, revoke, cleanup on disconnect.
4. **Tool surface.** Snapshot the tool list and flag write/destructive tools.
   Writes must go through the approval flow; never trust `readOnlyHint`-style
   annotations for gating.
5. **Result size.** Truncation and paging strategy to prevent context bloat.
6. **Rate limits.** Map 429/quota errors to readable tool errors with backoff.
7. **Availability.** Degrade gracefully when the server is unreachable (tool
   returns a readable error; the session is not blocked). Add health probing.
8. **Audit.** Log every call: user, server, tool, result summary.
9. **Regression.** Tool enumeration, error mapping and approval gating go in
   CI; OAuth flows are exercised against a test account in staging on a
   schedule.

## P1 — follow-up

- **Google Workspace** (Gmail/Calendar/Drive/Docs/Sheets…) — vendor-official,
  remote-hosted, per-user OAuth, currently **preview** (`gmailmcp.googleapis.com`
  style endpoints). Native connector keeps carrying the load until GA; then
  migrate behind the same provider abstraction. Community fallback:
  `taylorwilsdon/google_workspace_mcp` (local stdio).
- **Slack** — official remote, but bot token auth, not per-user OAuth; needs a
  per-user workspace install adapter.
- **Microsoft 365 / Teams / Azure** ([microsoft/mcp](https://github.com/microsoft/mcp)) —
  Entra tenant model, tenant ID required; Azure DevOps remote MCP launched
  with client restrictions (not usable from Claude/ChatGPT/Cursor at launch).
- **Local stdio servers** (need sandbox hosting + lifecycle management):
  Grafana, Home Assistant, AWS ([awslabs/mcp](https://github.com/awslabs/mcp),
  30+ servers), Docker MCP Gateway.
- **Data platforms**: Snowflake (managed MCP), Databricks (managed MCP);
  Neon (official but README says not production-ready).
- **Other official**: PayPal, Brave Search, Datadog.
- **Community database servers** (e.g. `crystaldba/postgres-mcp`) —
  connection-string credentials grant full DB access; requires security review
  before admission.

## P2 — fallback gateways

Zapier MCP (thousands of apps, per-app authorization), Composio. The official
MCP Registry, Smithery and mcp.so are discovery channels only — connecting
still goes through this allowlist. Note the official `servers` repo keeps only
a handful of reference servers; production servers live with their vendors.

## Build natively / DSL (MCP gaps)

Categories with no maintained official MCP server — candidates for native
connectors or the config-driven DSL layer:

- Discord, Telegram
- Trello, Monday
- Zendesk, Intercom, Shopify (no official server verified)
- Postgres / MySQL native read-only
- Consumer: Spotify, Google Maps
- Kubernetes, Vercel

## Maintenance

- **Quarterly review** of GA/preview status (Google Workspace first), endpoint
  drift, and vendor repo moves.
- **Admission criteria**: vendor-official first; community implementations need
  active maintenance plus a security review; remote-hosted over local stdio.
- **Protocol migration watch**: ecosystem is moving from 2025-11-25 to the
  stateless 2026-07-28 revision (sessions/handshake removed, Sampling/Roots
  deprecated, Tasks extension for async work). Track which P0 servers migrate.
