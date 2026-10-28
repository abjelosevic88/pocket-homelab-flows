# Configuration

Environment variables; every one has a default. Personal settings (notifications, connections, AI, history length) live in the UI per person — the `NOTIFY_*`/`OPENAI_*` values below are copied once into the first admin's settings.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (webhook addresses, links in pushes, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-flows.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB). Webhook bodies are capped at 1 MB. |
| `TZ` | – | Time zone schedules run in (mount `/usr/share/zoneinfo` on Alpine) |
| `WORKERS` | `4` | How many runs go at the same time (1–16) |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | “Ask AI” steps and drafting flows without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core: sign-in, events from your Pocket apps, app links, Pocket AI |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed twelve flows with two months of events and runs into the owner account (screenshots, trying it out) |

Commands: `pocket-flows backup [-o FILE]`, `pocket-flows reset-password [-email] [-password]`, `pocket-flows version`.

## Limits

A flow has at most 30 steps and 20 conditions per list; a person at most 500 flows. A flow runs at most 500 times an hour; a webhook address takes at most 60 calls a minute. A step gets 3 minutes; a wait lasts at most 30 days. Steps that fail for a reason that may pass (the other app is down, a 5xx, a network error) are tried again after 1, 5 and 20 minutes.

## Web steps and the local network

“Call a web address” steps of **members** cannot reach private addresses (loopback, 10/8, 172.16/12, 192.168/16, link-local, 100.64/10 — the tailnet) unless an admin switches on *Settings → General → Web steps may reach the local network*. The check happens after DNS, so a public name that resolves to a private address is refused too. Admins' steps always can.
