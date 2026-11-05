# FREE LLM install token (cloud alignment)

Roundpen’s built-in FREE path authenticates to your cloud LLM entry with a
**per-install** Ed25519 identity. There is no product private key in the binary.

## Files on disk

Under `$ROUNDPEN_DATA_ROOT/install-identity/`:

| File | Mode | Contents |
| --- | --- | --- |
| `install_id` | 0644 | Host device id when available (`/etc/device_id`, then trim/systemd machine-id), else a UUID |
| `ed25519` | 0600 | hex-encoded Ed25519 private key |
| `ed25519.pub` | 0644 | hex-encoded public key (TOFU registration) |

## Token format

```text
rp1.<install_id>.<exp_unix>.<base64url(sig)>
```

Signing message (exact bytes):

```text
rp1|<install_id>|<exp_unix>
```

`sig = Ed25519-Sign(private_key, message)`.

Roundpen mints short-lived tokens (`ROUNDPEN_FREE_LLM_TOKEN_TTL`, default `1h`)
and sends them as the OpenAI-compatible `Authorization: Bearer` value when
relaying to the `roundpen-free` provider (`authMode=install_identity`).

## Cloud verification (pseudo)

1. Parse four dot-separated parts; require prefix `rp1`.
2. Reject if `now > exp_unix`.
3. Look up `install_id`. First sighting: store the client’s public key (TOFU),
   optionally also keyed by IP to slow mass registration.
4. Later requests: if the presented public key matches, verify
   `Ed25519-Verify(pub, message, sig)`. If the key differs (NAS reinstall/wipe),
   accept the new key, increment `reinstallCount` for that `install_id`, then verify.
5. Rate-limit by `install_id` (and optionally IP). Return HTTP 429 when over quota;
   Roundpen surfaces that to the UI as a cue to switch to BYOK.

## Test vector

Stable vector from `go test ./internal/installid/ -run TestVector_KnownKey`:

| Field | Value |
| --- | --- |
| seed (hex, 32 bytes) | `0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef` |
| install_id | `11111111-2222-3333-4444-555555555555` |
| exp_unix | `1893456000` (2030-01-01T00:00:00Z) |
| message | `rp1\|11111111-2222-3333-4444-555555555555\|1893456000` |
| public_key_hex | `207a067892821e25d770f1fba0c47c11ff4b813e54162ece9eb839e076231ab6` |
| token | `rp1.11111111-2222-3333-4444-555555555555.1893456000.xN_w9bG-00T3TQX39B40w1DMmLkn0WEid4wGY5tSeCYwhd0wQR_6YUniaX1qkQEBPL-YK6-jrRKFroqxLzS8CA` |

## Env (Roundpen side, optional overrides)

FREE ships with a fixed cloud entry (`https://auth.eaxi.com/llm`, model
`roundpen-free`) on the same Cloudflare Worker as the OAuth relay. Clients call
`{base}/v1/chat/completions`. No env is required for cold-start.

| Env | Meaning |
| --- | --- |
| `ROUNDPEN_FREE_LLM_BASE_URL` | Override cloud OpenAI-compatible entry |
| `ROUNDPEN_FREE_LLM_MODEL` | Override default model name |
| `ROUNDPEN_FREE_LLM_TOKEN_TTL` | Go duration for token lifetime (default `1h`) |

Roundpen sends `Authorization: Bearer <rp1…>` and `X-Roundpen-Install-Pub: <hex>`
so the Worker can TOFU-register the install public key (shared with OAuth).

Cold start is zero-config: on boot, `EnsureDefaultFree` enables the gateway and
seeds item `roundpen-free` (binding `llm.default` / `llm.sysagent`) when those
slots have no binding yet. Admins can still force-seed via `POST /v1/setup/llm-free`.
When FREE returns HTTP 429, the UI cues **系统管理 → 模型配置** (BYOK), not personal settings.

## Reinstall / public key rotation

`install_id` prefers a stable host device id (`/etc/device_id`, …). Wiping
`$ROUNDPEN_DATA_ROOT/install-identity/` (or a fresh fnOS data dir after
uninstall) regenerates the Ed25519 keypair while keeping the same `install_id`.
The cloud entry **accepts** the new public key and increments `reinstallCount`
for that install — FREE and OAuth keep working after the next signed request.
Local app data wiped by uninstall is a separate concern; only the install
identity binding is rotated.

**Maintainer path (optional):** `POST /admin/installs/reset-tofu` clears the
stored pubkey (reinstall count kept) so the next request re-registers.
