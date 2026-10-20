# MCP connectors

Roundpen can connect third-party **MCP servers** (Notion, Linear, GitHub, …)
so the System Agent gains their tools. MCP is the extension layer for the long
tail of SaaS; native connectors (Google, see `docs/connectors.md`) stay
first-class. The server allowlist and compatibility baseline live in
`docs/mcp-catalog.md`.

## Model

| Piece | Who configures it |
|-------|-------------------|
| MCP server choice | Fixed allowlist (P0 catalog in `internal/mcp/catalog.go`) |
| API key servers (Exa, or Linear/GitHub/Stripe via key) | User pastes their personal key under **Settings → MCP** |
| OAuth servers | User clicks Connect; Roundpen self-registers an OAuth client (DCR) and runs PKCE |
| Agent tools | Dynamically registered per session from the user's connected servers |

## Connections

Two auth kinds:

- **API key** (`auth_kind=api_key`): the key is verified with a live
  `tools/list` before the connection is stored. Sent as
  `Authorization: Bearer <key>` on every MCP request.
- **OAuth** (`auth_kind=oauth`): standard MCP authorization —
  RFC 9728 protected-resource discovery (WWW-Authenticate challenge, with a
  `/.well-known/oauth-protected-resource` fallback) → RFC 8414 auth-server
  metadata → RFC 7591 dynamic client registration → authorization code + PKCE
  (S256) → token exchange with RFC 8707 `resource`. Refresh happens
  transparently within 5 minutes of expiry; a persistent 401 flips the
  connection to `needs_reauth` and the UI asks the user to reconnect.

Servers that expose no registration endpoint return
"automatic client registration not supported" (BYO OAuth clients are not
implemented yet). Only Streamable HTTP is used — SSE and stdio are out of
scope, and every outbound dial goes through the shared SSRF guard
(loopback/link-local/private addresses blocked).

## Agent behavior

- Session start snapshots the tool lists of the user's connected servers
  (3s budget, failures degrade to no MCP tools — a session never blocks on a
  dead server).
- Tools register as `mcp_<server>_<tool>` (sanitized to `[a-z0-9_]`, ≤64
  chars, numeric suffix on collisions); descriptions are prefixed
  `[MCP: <Server>]`.
- **Every MCP tool is Mutating.** Server-side annotations such as
  `readOnlyHint` are never trusted for gating, so the first call of each tool
  per chat session goes through the normal approval prompt (allow-tool
  decisions are remembered for the session).
- **Scheduled runs (standing routines) never get MCP tools** — no approver
  exists there.
- Results are truncated to 32 KiB with a `[Content truncated due to length]`
  note. Transport failures map to readable guidance (unreachable / rate
  limited with Retry-After / reconnect needed); tool-level errors are
  returned as content (`MCP tool error: …`) so the model can self-correct.
- Every call is audited (`mcp.tool.call` with server, tool, ok, duration —
  never arguments or results).

## Storage

Tables (see `migrations/0025_mcp_connectors.sql`; runtime schema in
`internal/storage/schema/postgres.sql`):

- `user_mcp_connections` — per-user connection: auth kind, status, secretbox-
  sealed API key / tokens / DCR client secret, tool snapshot (`tools` JSONB),
  last error. `UNIQUE (user_id, server_id)`.
- `mcp_oauth_states` — single-use OAuth flow context (discovery result, DCR
  client, PKCE verifier), 10-minute TTL, consumed inside a transaction.

Secrets are sealed with the instance secretbox (`enc:v1:` prefix) when one is
configured; plaintext only when the box is absent (tests).

## API (authenticated unless noted)

| Method | Path |
|--------|------|
| GET | `/v1/me/mcp/catalog` |
| GET | `/v1/me/mcp/connections` |
| POST | `/v1/me/mcp/connections` (API key: `{serverId, apiKey}`) |
| DELETE | `/v1/me/mcp/connections/{serverId}` |
| POST | `/v1/me/mcp/connections/{serverId}/refresh-tools` |
| POST | `/v1/me/mcp/connections/{serverId}/oauth/start` |
| GET | `/v1/connectors/mcp/callback` (public; the state row binds the user) |

Responses never include secret material. Disconnect removes the connection and
best-effort revokes the grant (RFC 7009) when the AS advertises a revocation
endpoint.

## Not in this version

- BYO OAuth clients (pre-registered client id/secret) for servers without DCR.
- P1/P2 servers from `docs/mcp-catalog.md`, stdio transports, and health
  polling (status updates come from real calls).
- GitHub / GitLab as MCP servers — use Connected apps (OAuth or host + PAT)
  and `gh` / `glab` in the agent sandbox instead.
