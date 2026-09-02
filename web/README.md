# Web console (developers)

SPA source for the Roundpen console. Production assets go to `internal/ui/dist`
and are embedded into `roundpend`.

## Prefer `make dev`

```bash
make setup   # once
make dev     # pg0 + roundpend :19001 + Vite :19000
# open http://127.0.0.1:19000/
```

End users never run these steps — use `docker compose up` or a prebuilt binary.

## Manual hot reload

```bash
# terminal 1 (API already on :19001, or adjust vite proxy)
go run ./cmd/roundpend

# terminal 2
cd web && npm install && npm run dev
```

## Production embed

```bash
make build-ui    # → internal/ui/dist
make build-go    # embed + link
# or: make build
```

Docker hides Node: `deploy/Dockerfile` runs npm only in a build stage.
