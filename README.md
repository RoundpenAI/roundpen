# Roundpen

[中文](README.zh-CN.md)

**Always-on AI assistants on your NAS or small server.** Install once; assistants chat, work in Docker sandboxes, browse the web, track issues, and wake on a schedule — without you holding a chat open. One Go binary (`roundpend`) with an embedded Web console; ships via [Docker Compose](deploy/compose/README.md) or [fnOS](deploy/fnos/README.md).

Your data and secrets stay where you install it. Bring your own LLM keys when you want; a built-in free model is available out of the box so you can start talking immediately.

## What you get today

- **Assistants** — multiple personas with their own chats, skills, and capability grants
- **Isolated Agent + Browser** — code and shell in a Docker agent sandbox; browsing via a managed browser (or your own CDP endpoint)
- **Projects** — bind a repo, run a coordinator, and spin worker agents for larger jobs
- **Issues & standing routines** — one-off work items plus recurring jobs that run without you watching the chat
- **Home / NAS scene pack** — photo / files / media assistants with directory grants and download backends
- **LLM gateway** — built-in FREE tier on install; OpenAI / Anthropic–compatible upstreams when you need more
- **Web console** — day-to-day UI embedded in `roundpend`
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

## Binary

| Command | Role |
| --- | --- |
| `roundpend` | Control plane (embeds the Web UI) |

Loads `.env` from the **current working directory**; real environment variables win. Under systemd, use `EnvironmentFile=`.

## Where we're headed

Roundpen is a durable **personal / home-server AI workbench**:

- Standing routines and proactive digests that keep working while the console is closed
- Deeper home / NAS integration (directory grants, scene-pack assistants, local downloaders)
- Longer, segmented runs and stronger project coordination when jobs need more than one chat turn
- Longer-term: optional desktop / phone VM slots (QEMU) for agents that need a full GUI

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

## License

Apache License 2.0 — see [LICENSE](LICENSE).
