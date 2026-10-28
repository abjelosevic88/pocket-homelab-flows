# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer pfl_…`, Settings → API & shortcuts), a Pocket Core token, or a Pocket Core link token (`pcx_…`, only the routes of its scope). Everything is per person: other people's ids answer 404.

## Flows

| Route | What |
|---|---|
| `GET /flows` | Your flows (with `trigger_text` and, for webhook flows, `hook_url`) |
| `POST /flows` | Create a flow (below) |
| `GET/PUT/DELETE /flows/{id}` | One flow; PUT takes any subset of the fields |
| `POST /flows/{id}/enabled` `{enabled}` | Switch it on or off |
| `POST /flows/{id}/run` `{input}` or `{event_id}` | Run it now with this input (it becomes `{{trigger…}}`), or with one of your recorded events. `202` with the queued run |
| `POST /flows/{id}/duplicate` | A copy, switched off |
| `POST /flows/{id}/hook` | A new webhook address (the old one stops working) |
| `GET /flows/{id}/fields?kind&topic` | Placeholders a flow can use: the trigger's fields from the newest matching event or call |

```json
{"name": "Wish got cheaper", "enabled": true, "trigger_kind": "event",
 "trigger": {"topic": "wishlist.price_dropped", "conditions": [{"field": "price", "op": "lt", "value": "10000"}]},
 "steps": [{"id": "s1", "kind": "notify", "config": {"title": "Cheaper: {{wish}}", "body": "Now {{price | cents}} {{currency}}", "url": "{{url}}", "priority": "high"}}]}
```

**Triggers.** `event` — `topic` (`app.event`, `app.*`, or several, comma-separated) plus optional `conditions` (`match: "any"` for or). `schedule` — `repeat` `minutes` (`every` 5–720), `hourly` (`every` 1–24, `time` `00:MM`), `daily` (`time`), `weekly` (`days` ISO weekdays `"12345"`, `time`), `monthly` (`day_of_month`, `time`), `once` (`at` `YYYY-MM-DDTHH:MM`), in the server's time zone. `webhook` — optional `conditions`. `manual`.

**Steps.** `notify`, `http`, `ai`, `wait`, `filter` (stops the run quietly unless `only_if` holds) and, in sibling apps, `tasks.add`, `notes.capture`, `notes.create`, `calendar.add`, `money.add`, `habits.mark`, `inventory.adjust`, `projects.issue`. Every step may have `only_if` conditions (skipped when they do not hold) and `on_error: "continue"`. `GET /catalog` lists every kind with its fields, the apps with their events, and the comparisons.

**Conditions** (`op`): `equals`, `not_equals`, `contains`, `not_contains`, `starts_with`, `ends_with`, `gt`, `gte`, `lt`, `lte`, `exists`, `missing`, `matches` (a regular expression). Numbers compare as numbers (`"12,50"` too).

**Placeholders** `{{path | filter}}`: `trigger.*` (bare names mean the trigger: `{{title}}`), `steps.<id>.*` (an earlier step's output), `me.name`, `me.email`, `flow.name`, `flow.url`, `now`, `today`, `tomorrow`, `time`, `weekday`. Filters: `upper`, `lower`, `trim`, `default: x`, `json`, `truncate: n`, `first_line`, `round: n`, `cents` (minor units → 12.50), `date: DD.MM.YYYY`, `count`.

## Webhooks

`POST` (or `GET`/`PUT`) `/hooks/<secret>` — no token: the address is the secret. A JSON object's fields become the trigger; a JSON list becomes `items`, a form its fields, anything else `text`; query values are added (and under `query`). Answers `202 {"started": true, "run_id"}`, `200 {"started": false}` when the conditions do not hold, `404` for an unknown address, `409` when the flow is off, `429` past 60 calls a minute. Send `Idempotency-Key` to make a retried call start only one run.

## Runs and events

| Route | What |
|---|---|
| `GET /runs?flow&status&before&limit` | Newest first; `status` `ok`, `failed`, `stopped`, `active` (queued, running, waiting) |
| `GET /runs/{id}` | One run with its input, every step's output and log |
| `POST /runs/{id}/retry` | Try a failed (or cancelled) run again from the step that failed |
| `POST /runs/{id}/cancel` | Cancel a waiting or queued run |
| `GET /events?topic&before&limit` | Events from your Pocket apps that Flows heard (30 days) |
| `GET /events/topics` | Topics you have events for, with counts |
| `GET /overview` | Runs today and this week, failures, the last 14 days, what runs next, the event stream's state |

## Everything else

| Route | What |
|---|---|
| `GET /catalog` | Apps (events, whether Flows hears them and can act in them for you), step kinds, comparisons |
| `GET /pickers?name=tasks.lists` | A list from a sibling app for a step's select (`tasks.lists`, `calendar.calendars`, `money.accounts`, `money.categories`, `habits.habits`, `inventory.items`, `projects.teams`, `notes.folders`) |
| `GET /recipes`, `POST /recipes/{key}` | Ready-made flows; using one creates it switched off |
| `POST /import/n8n` | An n8n workflow export (one, a list, or `{"data": [...]}`) → flows, switched off; idempotent |
| `POST /ai/draft` `{text}` | Draft a flow from a sentence (not saved) |
| `GET /ai/status`, `GET /ai/models` | |
| `GET/PUT /settings` | Your settings; `connections: {"tasks": {"mode": "direct", "url": "…", "token": "…"}}` |
| `GET /export.json`, `POST /import` | Your flows; importing twice adds nothing |
| `GET /export.csv` | Your run history |
| `GET /backup` | The whole database (admins) |

Public: `/healthz` (`{status, version, uptime, core_outbox}`), `/readyz`, `/metrics` (`pocket_flows_*`), `/pocket-ask.js`.

## Pocket Core

Uses, per app: `events:read` (to start flows from that app's events — only events that name you in `user_email`) and the scopes its steps need (`tasks:write`, `notes:write`, `calendar:write`, `transactions:write`, `entries:write`, `items:adjust`, `projects:write`, plus the matching `:read` for pickers), and Pocket AI. Offers `flows:read` (flows and runs, never webhook addresses) and `flows:run` (start a flow that runs by hand). Events: `flows.run_finished`, `flows.run_failed`, `flows.flow_created`. Flows never starts a flow from its own events.
