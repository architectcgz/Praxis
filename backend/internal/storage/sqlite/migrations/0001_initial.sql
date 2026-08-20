CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS task_sessions (
    id TEXT PRIMARY KEY,
    workspace_key TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'archived')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS task_packets (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS context_manifests (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS capability_grants (
    id TEXT PRIMARY KEY,
    workspace_key TEXT NOT NULL,
    context_manifest_id TEXT NOT NULL,
    approval_source TEXT NOT NULL CHECK (approval_source IN ('user', 'policy_default')),
    approval_policy_fingerprint TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS delegation_requests (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL,
    source_thread_id TEXT NOT NULL DEFAULT '',
    profile TEXT NOT NULL,
    task_packet_id TEXT NOT NULL,
    context_manifest_id TEXT NOT NULL,
    capability_grant_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected', 'cancelled')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_threads (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL,
    profile TEXT NOT NULL,
    task_packet_id TEXT NOT NULL,
    context_manifest_id TEXT NOT NULL,
    capability_grant_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('idle', 'running', 'pausing', 'paused', 'interrupted', 'failed', 'closed')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    agent_thread_id TEXT NOT NULL,
    work_item_id TEXT NOT NULL DEFAULT '',
    sandbox_mode TEXT NOT NULL,
    approval_mode TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_write_leases (
    id TEXT PRIMARY KEY,
    workspace_key TEXT NOT NULL,
    owner_thread_id TEXT NOT NULL,
    capability_grant_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'released')),
    payload TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS workspace_write_lease_one_active_per_workspace
    ON workspace_write_leases (workspace_key)
    WHERE state = 'active';

CREATE TABLE IF NOT EXISTS agent_results (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL,
    source_thread_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS briefings (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL,
    source_thread_id TEXT NOT NULL,
    target_thread_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS briefing_deliveries (
    id TEXT PRIMARY KEY,
    briefing_id TEXT NOT NULL,
    target_thread_id TEXT NOT NULL,
    injection_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivering', 'delivered', 'failed')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notes (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL,
    source_thread_id TEXT NOT NULL,
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS orchestration_events (
    id TEXT PRIMARY KEY,
    task_session_id TEXT NOT NULL DEFAULT '',
    agent_thread_id TEXT NOT NULL DEFAULT '',
    agent_run_id TEXT NOT NULL DEFAULT '',
    work_item_id TEXT NOT NULL DEFAULT '',
    delegation_request_id TEXT NOT NULL DEFAULT '',
    delivery_id TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL
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
