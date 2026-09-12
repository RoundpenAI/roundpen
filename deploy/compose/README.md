# Local / NAS Compose stack

End users do **not** need Node or Go. The console SPA is built inside
`deploy/Dockerfile` and embedded into `roundpend`.

```bash
# from repo root
cp .env.compose.example .env   # optional
docker compose up -d --build
# open http://127.0.0.1:9527 — admin password is in roundpend logs once
docker compose logs roundpend | head
```

Services:

| Service | Role |
|---------|------|
| `postgres` | PostgreSQL 16 + pgvector |
| `roundpend` | Control plane + embedded Web UI |

Default backend is `docker`: Agent containers run on the host Docker Engine via
`/var/run/docker.sock` (mounted by `compose.yaml`). The official Agent image
`ghcr.io/roundpenai/code-agent:0.1.0` is pulled on first start (override with
`ROUNDPEN_AGENT_IMAGE`, or `docker load -i code-agent.tar` offline). Browser /
Desktop slots use QEMU on the host.

Dev without Compose: `make setup && make dev`（pg0 + API + Vite；见 `scripts/dev-up.sh`）。
