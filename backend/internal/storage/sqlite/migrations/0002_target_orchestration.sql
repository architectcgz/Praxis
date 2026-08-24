CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    workspace_key TEXT NOT NULL,
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_groups (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    primary_agent_id TEXT NOT NULL DEFAULT '',
    max_concurrent INTEGER NOT NULL CHECK (max_concurrent > 0),
    payload TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS agent_groups_by_session
    ON agent_groups (session_id, id);

CREATE UNIQUE INDEX IF NOT EXISTS agent_groups_session_identity
    ON agent_groups (id, session_id);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    profile TEXT NOT NULL,
    task_packet_id TEXT NOT NULL,
    context_manifest_id TEXT NOT NULL,
    capability_grant_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN (
        'idle', 'executing', 'waiting', 'pausing', 'paused', 'interrupted', 'failed', 'closed'
    )),
    current_execution_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL,
    FOREIGN KEY (group_id, session_id) REFERENCES agent_groups (id, session_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS agents_by_group
    ON agents (group_id, id);

CREATE UNIQUE INDEX IF NOT EXISTS agents_session_identity
    ON agents (id, session_id);

CREATE TABLE IF NOT EXISTS agent_executions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    work_item_id TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL CHECK (reason IN ('user_input', 'queued_work', 'context_delivery', 'resume')),
    status TEXT NOT NULL CHECK (status IN ('starting', 'running', 'settling', 'settled')),
    outcome TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    settled_at TEXT,
    payload TEXT NOT NULL,
    UNIQUE (agent_id, request_id),
    FOREIGN KEY (agent_id, session_id) REFERENCES agents (id, session_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS agent_executions_one_active_per_agent
    ON agent_executions (agent_id)
    WHERE status IN ('starting', 'running', 'settling');

CREATE INDEX IF NOT EXISTS agent_executions_recovery
    ON agent_executions (status, created_at, id);

CREATE TABLE IF NOT EXISTS wait_conditions (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('delivery', 'approval', 'child')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'resolved', 'cancelled')),
    payload TEXT NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE RESTRICT,
    FOREIGN KEY (execution_id) REFERENCES agent_executions(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS wait_conditions_by_agent
    ON wait_conditions (agent_id, status, id);

CREATE TABLE IF NOT EXISTS agent_control_requests (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    target_execution_id TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('pause', 'close')),
    status TEXT NOT NULL CHECK (status IN ('requested', 'applied')),
    payload TEXT NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE RESTRICT,
    FOREIGN KEY (target_execution_id) REFERENCES agent_executions(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS agent_control_requests_open
    ON agent_control_requests (agent_id, status, id);

CREATE TABLE IF NOT EXISTS context_deliveries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    source_artifact_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'pending', 'delivering', 'delivered', 'rejected', 'cancelled', 'failed'
    )),
    payload TEXT NOT NULL,
    UNIQUE (target_agent_id, dedupe_key),
    FOREIGN KEY (target_agent_id, session_id) REFERENCES agents (id, session_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS context_deliveries_pending_by_target
    ON context_deliveries (target_agent_id, status, id);
