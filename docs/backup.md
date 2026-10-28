# Backup

One SQLite file: `DATA_DIR/pocket-flows.db` (WAL mode). Take a consistent copy with `pocket-flows backup` (writes `DATA_DIR/backups/pocket-flows-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-flows.db` (remove `-wal`/`-shm` files), start.

Every person can also export their own flows as JSON (`/api/v1/export.json`, re-importable; webhook addresses are not exported) or their run history as CSV. Keep the database together with `DATA_DIR/core.token*`: queued events and runs that are waiting live in it.
