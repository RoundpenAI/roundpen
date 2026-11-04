# Observability (OpenTelemetry → SigNoz)

`roundpend` exports traces, a small set of RED metrics, and **error/warn logs** over **OTLP/gRPC** by default.

## Default

| Setting | Value |
|---------|--------|
| Endpoint | `https://otlp-grpc.ttz.ac.cn` |
| Protocol | `grpc` |

Opt out:

```bash
export ROUNDPEN_OTEL_ENABLED=false
```

Override collector / use OTLP/HTTP instead:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.ttz.ac.cn
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
```

## Distinguishing NAS installs

Every install shares `service.name=roundpend`. SigNoz filters must use Resource attributes:

| Attribute | Meaning |
|-----------|---------|
| `service.instance.id` | Same as `install_id` (authoritative) |
| `roundpen.install_id` | Duplicate for readable queries |
| `host.name` | Hostname (not unique) |
| `deployment.environment` | From `ROUNDPEN_OTEL_ENV` / `OTEL_RESOURCE_ATTRIBUTES` |
| `roundpen.channel` | `fnos` / `compose` / `dev` |

Example: filter traces where `service.instance.id = "<install_id from data/install/install_id>"`.

## First-batch signals

**Traces**

- HTTP (otelhttp; `/health` and SPA assets filtered)
- `agent.turn` / `acp.prompt` (+ `ttft_ms`)
- `llmgw.relay` (bridges existing `request_id`)
- `sandbox.create`, `docker.image.pull`, `browser.cdp.ready`

**Logs (SigNoz Logs)**

- `slog` **Warn+** (stdout JSON still always on)
- Span failures via `otelx.EndSpan` (error body + trace correlation)
