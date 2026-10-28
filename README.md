# Roundpen

[中文](README.zh-CN.md)

**Self-hosted AI workbench.** Run assistants on your own hardware — they chat, write code in Docker sandboxes, browse the web, track work, and wake up on a schedule. One Go binary; works on a laptop, a Linux server, or a home NAS (including [fnOS](deploy/fnos/README.md)).

Your data and secrets stay where you install it. Bring your own LLM keys when you want; a built-in free model is available out of the box so you can start talking immediately.

## What you get today

- **Assistants** — multiple personas with their own chats, skills, and capability grants
- **Isolated Agent + Browser** — code and shell in a Docker agent sandbox; browsing via a managed browser (or your own CDP endpoint)
- **Projects** — bind a repo, run a coordinator, and spin worker agents for larger jobs
- **Issues & standing routines** — one-off work items plus recurring jobs that run without you watching the chat
- **LLM gateway** — built-in FREE tier on install; OpenAI / Anthropic–compatible upstreams when you need more
- **Web console + mobile app** — day-to-day UI in the browser; Flutter client for phone
- **Connectors** — Git / OAuth, MCP, IM channels, web search, and more from Settings

## Quick start

### Docker Compose (recommended)

No local Node or Go required — the console is baked into the image:

```bash
cp .env.compose.example .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env
docker compose up -d --build
# Open http://127.0.0.1:9527
# Initial admin password: docker compose logs roundpend | head
```

Details: [deploy/compose/README.md](deploy/compose/README.md).

### fnOS (飞牛)

Install the `.fpk` package — see [deploy/fnos/README.md](deploy/fnos/README.md). After fnOS login you can use the built-in free model; when the quota runs out, add your own key under **System admin → LLM gateway**.

### Local development

```bash
make setup && make dev
# API :19001 · Vite console http://127.0.0.1:19000/
```

Environment variables: [.env.example](.env.example) / [.env.compose.example](.env.compose.example).

## After install

| You want to… | Go to |
| --- | --- |
| Chat / create assistants | Console `/a` |
| Free model / your API keys | **System admin → LLM gateway** |
| Change the Agent image | **System admin → General** (or `ROUNDPEN_AGENT_IMAGE`) |
| Personal Git / linked accounts | **Settings** |
| Issues / standing routines / projects | `/issues`, assistant detail, `/projects` |

Agent and Browser both run in Docker. First use pulls:

- Agent: `ghcr.io/roundpenai/code-agent:0.1.0` (`ROUNDPEN_AGENT_IMAGE` or `docker load`)
- Browser: `ghcr.io/browserless/chrome:v2.56.7` (`ROUNDPEN_BROWSER_IMAGE`; prefetched on install/start when possible)

If `ghcr.io` is unreachable, Roundpen tries domestic mirror registries (`ROUNDPEN_IMAGE_MIRRORS`) and retags on success. Offline / air-gapped installs can `docker load` — no Dockerfile required on the host.

## Binaries

| Command | Role |
| --- | --- |
| `roundpend` | Control plane (embeds the Web UI) |
| `roundpen` | CLI |

Both load `.env` from the **current working directory**; real environment variables win. Under systemd, use `EnvironmentFile=`.

## Where we're headed

Roundpen is evolving from “chat with an agent on my box” into a durable **personal / small-team AI workbench**:

- Longer, segmented runs that survive across hours without holding a single prompt open
- Stronger project coordination (multi-agent workers, shared project context)
- Deeper home / NAS integration (directory grants, local services)
- Optional desktop / phone VM slots for agents that need a full GUI environment

The north star stays the same: **capable assistants that live on infrastructure you control**, not a cloud you rent by the minute.

## Docs

| Doc | About |
| --- | --- |
| [docs/architecture/project-layout.md](docs/architecture/project-layout.md) | Repository layout |
| [docs/architecture/browser-env.md](docs/architecture/browser-env.md) | Browser sources (managed / LAN / cloud / host Chrome) |
| [docs/workspace-layout.md](docs/workspace-layout.md) | Workspace directories |
| [docs/security.md](docs/security.md) | Isolation and policy |
| [docs/auth.md](docs/auth.md) | Login and OAuth |
| [docs/free-llm-token.md](docs/free-llm-token.md) | Built-in FREE model identity and rate limits |
| [docs/connectors.md](docs/connectors.md) / [docs/mcp-connectors.md](docs/mcp-connectors.md) | Google / MCP connectors |
| [docs/skills.md](docs/skills.md) | Reusable skill workflows |
| [mobile/](mobile/) | Mobile console (Flutter) |

## License

Apache License 2.0 — see [LICENSE](LICENSE).
