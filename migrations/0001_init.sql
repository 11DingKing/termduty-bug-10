-- Initial schema for the terminal & case time-limit monitoring service.
-- Storage shape: sharded JSONL files for readings (source of truth) plus an
-- embedded SQLite index/manifest holding the structured business records.

CREATE TABLE IF NOT EXISTS schema_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO schema_meta (key, value, updated_at)
VALUES ('version', '1', strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT OR IGNORE INTO schema_meta (key, value, updated_at)
VALUES ('data_format', '1', strftime('%Y-%m-%dT%H:%M:%SZ','now'));

CREATE TABLE IF NOT EXISTS collectors (
    id           TEXT PRIMARY KEY,
    code         TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    location     TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'active',
    handler_id   TEXT NOT NULL DEFAULT '',
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rules (
    id             TEXT PRIMARY KEY,
    collector_id   TEXT,
    metric         TEXT NOT NULL,
    window_seconds INTEGER NOT NULL DEFAULT 60,
    min_value      REAL,
    max_value      REAL,
    fault_trigger  TEXT NOT NULL DEFAULT '',
    severity       TEXT NOT NULL DEFAULT 'warn',
    enabled        INTEGER NOT NULL DEFAULT 1,
    version        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ingest_queue (
    id               TEXT PRIMARY KEY,
    collector_id     TEXT NOT NULL,
    payload          TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    leased_at        TEXT,
    lease_expires_at TEXT,
    attempts         INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT NOT NULL DEFAULT '',
    shard_id         TEXT NOT NULL DEFAULT '',
    line_no          INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ingest_status ON ingest_queue (status, created_at);
CREATE INDEX IF NOT EXISTS idx_ingest_lease ON ingest_queue (lease_expires_at);

CREATE TABLE IF NOT EXISTS reading_shards (
    shard_id    TEXT PRIMARY KEY,
    path        TEXT NOT NULL,
    bucket      TEXT NOT NULL,
    first_ts    TEXT,
    last_ts     TEXT,
    count      INTEGER NOT NULL DEFAULT 0,
    checksum    TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'ok',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reading_shards_bucket ON reading_shards (bucket, first_ts);

CREATE TABLE IF NOT EXISTS reading_index (
    id           TEXT PRIMARY KEY,
    collector_id TEXT NOT NULL,
    ts           TEXT NOT NULL,
    queue_count  INTEGER NOT NULL DEFAULT 0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    fault_code   TEXT NOT NULL DEFAULT '',
    shard_id     TEXT NOT NULL,
    line_no      INTEGER NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reading_lookup ON reading_index (collector_id, ts);
CREATE INDEX IF NOT EXISTS idx_reading_shard ON reading_index (shard_id, line_no);

CREATE TABLE IF NOT EXISTS alerts (
    id              TEXT PRIMARY KEY,
    collector_id    TEXT NOT NULL,
    rule_id         TEXT NOT NULL,
    reading_id      TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'warn',
    state           TEXT NOT NULL DEFAULT 'open',
    message         TEXT NOT NULL DEFAULT '',
    assignee_id     TEXT NOT NULL DEFAULT '',
    first_seen      TEXT NOT NULL,
    last_seen       TEXT NOT NULL,
    suppressed_until TEXT NOT NULL DEFAULT '',
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alerts_state ON alerts (state, created_at);
CREATE INDEX IF NOT EXISTS idx_alerts_collector ON alerts (collector_id, state);
CREATE INDEX IF NOT EXISTS idx_alerts_active ON alerts (collector_id, rule_id, state);

CREATE TABLE IF NOT EXISTS assignments (
    id          TEXT PRIMARY KEY,
    alert_id    TEXT NOT NULL,
    handler_id  TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'active',
    accepted_at TEXT NOT NULL,
    completed_at TEXT,
    note        TEXT NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_active_assignment ON assignments (alert_id) WHERE state = 'active';
CREATE INDEX IF NOT EXISTS idx_assignment_alert ON assignments (alert_id);
CREATE INDEX IF NOT EXISTS idx_assignment_handler ON assignments (handler_id, state);

CREATE TABLE IF NOT EXISTS audit (
    id          TEXT PRIMARY KEY,
    actor       TEXT NOT NULL,
    role        TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT '{}',
    at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_target ON audit (target_type, target_id, at);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit (actor, at);

CREATE TABLE IF NOT EXISTS permanent_failures (
    id          TEXT PRIMARY KEY,
    task_type   TEXT NOT NULL,
    target_id   TEXT NOT NULL DEFAULT '',
    payload     TEXT NOT NULL DEFAULT '',
    last_error  TEXT NOT NULL DEFAULT '',
    attempts    INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'dead',
    failed_at   TEXT NOT NULL,
    resolved    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_permfail_target ON permanent_failures (task_type, target_id);
CREATE INDEX IF NOT EXISTS idx_permfail_status ON permanent_failures (status, resolved);
