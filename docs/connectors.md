# Google connectors (Gmail + Calendar)

Roundpen can connect a user’s Google account for **read-only** Gmail and Calendar
access. This is separate from GitHub/Gitea OAuth login (`docs/auth.md`).

## Model

| Piece | Who configures it |
|-------|-------------------|
| Shared Google OAuth client | Official relay (`ROUNDPEN_OAUTH_RELAY`, default `https://auth.eaxi.com`) |
| Optional private Google client (Client ID + Secret) | The user. When set, it replaces the shared app for that user |
| Consent + tokens | User connects under **Settings → Connectors** |
| Agent tools | System Agent: `GmailSearch`, `GmailGet`, `CalendarListEvents`, `CalendarGetEvent` |

There are **no** send-mail / write-calendar tools in this MVP.

When the relay is on and the user has not saved a private client, **Connect Google**
starts at `POST /oauth/google/start`. The browser returns to
`/v1/connectors/google/cloud/callback`. The grant's `source` is `relay`, and
access-token refresh goes to `POST /oauth/google/refresh` so the client secret
stays on the worker. The relay binds the one-time ticket to this install and to
the NAS CSRF state, and binds the refresh token to this install. Set
`ROUNDPEN_OAUTH_RELAY=off` to require a private client. `ROUNDPEN_GITHUB_OAUTH_RELAY`
is still accepted as a legacy alias for the same origin.

## Setup (per user)

1. In [Google Cloud Console](https://console.cloud.google.com/), create (or reuse) a project.
2. Enable **Gmail API** and **Google Calendar API**.
3. Create an OAuth client of type **Web application**.
4. Under **Authorized redirect URIs**, add the URI shown in Roundpen
   **Settings → Connectors** (shape:
   `{scheme}://{host}/v1/connectors/google/callback`).
5. Paste Client ID and Client secret into Roundpen and save.
6. Click **Connect Google**, approve `gmail.readonly` and `calendar.readonly`
   (plus `openid` / `email` for the account label).

If the OAuth client is in “Testing” mode, only test users listed in the consent
screen can connect.

## Scopes

```
openid email
https://www.googleapis.com/auth/gmail.readonly
https://www.googleapis.com/auth/calendar.readonly
```

## Agent behavior

- Tools are registered whenever the control plane has the connectors service
  (always in a normal `roundpend` process).
- Calls use the signed-in user’s grant. If Google is not connected, tools return
  a clear error; the model should point the user at Settings → Connectors.
- Standing routines may use these tools (read-only) under both `read` and
  `browse` autonomy.

## Storage

Tables (see `migrations/0024_google_connectors.sql`):

- `user_google_oauth_clients` — per-user Client ID/Secret
- `user_connector_grants` — access/refresh tokens for kind `google`
- `connector_oauth_states` — single-use PKCE states

Tokens are stored in Postgres like federated-login identities (plaintext at rest).
Disconnect removes the grant but keeps the OAuth client credentials unless the
user removes the client.

## API (authenticated unless noted)

| Method | Path |
|--------|------|
| GET | `/v1/me/connectors` |
| GET/PUT/DELETE | `/v1/me/connectors/google/client` |
| POST | `/v1/me/connectors/google/connect` |
| DELETE | `/v1/me/connectors/google` |
| GET | `/v1/connectors/google/callback` (public; PKCE state binds the user) |
| GET | `/v1/connectors/google/cloud/callback` (public; relay ticket + NAS state) |
