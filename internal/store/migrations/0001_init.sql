CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);


-- A flow: "when this happens, do these things". Everything is per person.
CREATE TABLE flows (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '',
  enabled      INTEGER NOT NULL DEFAULT 1,
  trigger_kind TEXT NOT NULL DEFAULT 'manual',  -- event | schedule | webhook | manual
  trigger      TEXT NOT NULL DEFAULT '{}',      -- JSON, see store.Trigger
  steps        TEXT NOT NULL DEFAULT '[]',      -- JSON, see store.Step
  hook_token   TEXT,                            -- the secret part of a webhook flow's address
  next_run_at  TEXT NOT NULL DEFAULT '',        -- schedule flows: the next time it runs (UTC)
  last_run_at  TEXT NOT NULL DEFAULT '',
  last_status  TEXT NOT NULL DEFAULT '',
  run_count    INTEGER NOT NULL DEFAULT 0,
  fail_count   INTEGER NOT NULL DEFAULT 0,
  recipe       TEXT NOT NULL DEFAULT '',        -- the recipe it started from
  source       TEXT NOT NULL DEFAULT 'app',     -- app | import | n8n | <pocket app slug>
  external_id  TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX flows_user ON flows(user_id, enabled);
CREATE INDEX flows_due ON flows(trigger_kind, enabled, next_run_at);
CREATE UNIQUE INDEX flows_hook ON flows(hook_token) WHERE hook_token IS NOT NULL;

-- One run of a flow. Runs are durable: the step index and every step's
-- output are saved as it goes, so a restart (or a wait) continues where it was.
CREATE TABLE runs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  flow_id      INTEGER NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
  flow_name    TEXT NOT NULL DEFAULT '',
  trigger_kind TEXT NOT NULL DEFAULT '',
  trigger_ref  TEXT NOT NULL DEFAULT '',        -- event:<core id> | schedule:<time> | hook:<id> | manual:<id>
  summary      TEXT NOT NULL DEFAULT '',        -- one line about what started it
  input        TEXT NOT NULL DEFAULT '{}',      -- the trigger's data
  status       TEXT NOT NULL DEFAULT 'queued',  -- queued | running | waiting | ok | failed | stopped | cancelled
  step         INTEGER NOT NULL DEFAULT 0,      -- the next step to do
  attempt      INTEGER NOT NULL DEFAULT 0,      -- retries of the current step
  outputs      TEXT NOT NULL DEFAULT '{}',      -- step id -> output
  log          TEXT NOT NULL DEFAULT '[]',      -- what each step did
  error        TEXT NOT NULL DEFAULT '',
  resume_at    TEXT NOT NULL DEFAULT '',        -- waiting: when to go on (UTC)
  started_at   TEXT NOT NULL DEFAULT '',
  finished_at  TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX runs_user ON runs(user_id, id DESC);
CREATE INDEX runs_flow ON runs(flow_id, id DESC);
CREATE INDEX runs_todo ON runs(status, resume_at);
CREATE UNIQUE INDEX runs_once ON runs(flow_id, trigger_ref) WHERE trigger_ref != '';

-- Events from the Pocket Core bus that belong to a person (kept 30 days):
-- the Activity page, "try with the last event" and the fields a trigger offers.
CREATE TABLE events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  core_id     INTEGER NOT NULL DEFAULT 0,
  topic       TEXT NOT NULL,
  source      TEXT NOT NULL DEFAULT '',
  subject     TEXT NOT NULL DEFAULT '',
  payload     TEXT NOT NULL DEFAULT '{}',
  matched     INTEGER NOT NULL DEFAULT 0,       -- how many flows it started
  received_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX events_user ON events(user_id, id DESC);
CREATE INDEX events_user_topic ON events(user_id, topic, id DESC);
CREATE UNIQUE INDEX events_once ON events(user_id, core_id) WHERE core_id > 0;
