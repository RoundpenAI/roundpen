# Local / NAS Compose stack

End users do **not** need Node or Go. The console SPA is built inside
`deploy/Dockerfile` and embedded into `roundpend`.

```bash
# from repo root
cp .env.compose.example .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env   # required — no default DB password
docker compose up -d --build
# open http://127.0.0.1:9527 — admin password is in roundpend logs once
docker compose logs roundpend | head
```

Services:

| Service | Role |
|---------|------|
| `postgres` | PostgreSQL 16 + pgvector |
| `roundpend` | Control plane + embedded Web UI |

Default backend is `docker`: Agent and Browser containers run on the host Docker
Engine via `/var/run/docker.sock` (mounted by `compose.yaml`). The official Agent
image `ghcr.io/roundpenai/code-agent:0.1.0` and Browser image
`ghcr.io/browserless/chrome:v2.56.7` are preloaded in the background when
`roundpend` starts with a working Docker socket (override with
`ROUNDPEN_AGENT_IMAGE` / `ROUNDPEN_BROWSER_IMAGE`, or `docker load` offline).
If `ghcr.io` is unreachable (common on some networks), pulls automatically
retry via built-in mirrors (DaoCloud) or `ROUNDPEN_IMAGE_MIRRORS`, then retag
to the logical name. The Playwright driver is baked into the roundpend image.
Desktop / Mobile slots are reserved for QEMU on the host.

Dev without Compose: `make setup && make dev`（pg0 + API + Vite；见 `scripts/dev-up.sh`）。
