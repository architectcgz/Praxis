CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS work_queue_items (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL,
    agent_thread_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    prompt TEXT NOT NULL,
    task_packet_id TEXT NOT NULL,
    context_manifest_id TEXT NOT NULL,
    capability_grant_id TEXT NOT NULL,
    sandbox_mode TEXT NOT NULL,
    approval_mode TEXT NOT NULL,
    execution_revision TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'paused', 'failed', 'interrupted', 'completed', 'cancelled')),
    agent_run_id TEXT,
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    failure_code TEXT NOT NULL DEFAULT '',
    UNIQUE (agent_thread_id, sequence)
);

CREATE UNIQUE INDEX IF NOT EXISTS work_queue_one_running_per_thread
    ON work_queue_items (agent_thread_id)
    WHERE status = 'running';

CREATE INDEX IF NOT EXISTS work_queue_fifo_index
    ON work_queue_items (agent_thread_id, status, sequence, id);

INSERT OR IGNORE INTO schema_migrations (version, applied_at)
VALUES (1, '2026-08-17T00:00:00Z');
