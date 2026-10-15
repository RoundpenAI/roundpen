# Google connectors (Gmail + Calendar)

Roundpen can connect a user’s Google account for **read-only** Gmail and Calendar
access. This is separate from GitHub/Gitea OAuth login (`docs/auth.md`).

## Model

| Piece | Who configures it |
|-------|-------------------|
| Google Cloud OAuth client (Client ID + Secret) | Each Roundpen user (bring your own) |
| Consent + tokens | User connects under **Settings → Connectors** |
| Agent tools | System Agent: `GmailSearch`, `GmailGet`, `CalendarListEvents`, `CalendarGetEvent` |

There is **no** instance-wide Google OAuth app and **no** send-mail / write-calendar
tools in this MVP.

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
