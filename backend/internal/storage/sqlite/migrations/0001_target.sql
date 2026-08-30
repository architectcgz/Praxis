CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    default_workspace_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    FOREIGN KEY (default_workspace_id, id)
        REFERENCES workspaces(id, project_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('project_root', 'worktree', 'temporary')),
    path TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('ready', 'unavailable', 'archived')),
    revision INTEGER NOT NULL CHECK (revision > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    UNIQUE (id, project_id),
    UNIQUE (project_id, path)
);

CREATE INDEX IF NOT EXISTS workspaces_by_project ON workspaces(project_id, id);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    workspace_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    FOREIGN KEY (workspace_id, project_id) REFERENCES workspaces(id, project_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS sessions_by_project ON sessions(project_id, updated_at DESC, id);
CREATE INDEX IF NOT EXISTS sessions_by_workspace ON sessions(workspace_id, id);

CREATE TABLE IF NOT EXISTS agent_groups (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    primary_agent_id TEXT NOT NULL DEFAULT '',
    max_concurrent INTEGER NOT NULL CHECK (max_concurrent > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_groups_by_session ON agent_groups(session_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS agent_groups_session_identity ON agent_groups(id, session_id);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    profile TEXT NOT NULL,
    task_packet_id TEXT NOT NULL,
    context_manifest_id TEXT NOT NULL,
    capability_grant_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('idle', 'executing', 'waiting', 'pausing', 'paused', 'interrupted', 'failed', 'closed')),
    current_execution_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    FOREIGN KEY (group_id, session_id) REFERENCES agent_groups(id, session_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS agents_by_group ON agents(group_id, id);
CREATE INDEX IF NOT EXISTS agents_by_session ON agents(session_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS agents_session_identity ON agents(id, session_id);

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
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    workspace_path_snapshot TEXT NOT NULL,
    workspace_revision INTEGER NOT NULL CHECK (workspace_revision > 0),
    context_manifest_id TEXT NOT NULL REFERENCES context_manifests(id) ON DELETE RESTRICT,
    approval_source TEXT NOT NULL CHECK (approval_source IN ('user', 'policy_default')),
    approval_policy_fingerprint TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS delegation_requests (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    source_agent_id TEXT NOT NULL,
    profile TEXT NOT NULL,
    task_packet_id TEXT NOT NULL REFERENCES task_packets(id) ON DELETE RESTRICT,
    context_manifest_id TEXT NOT NULL REFERENCES context_manifests(id) ON DELETE RESTRICT,
    capability_grant_id TEXT NOT NULL REFERENCES capability_grants(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected', 'cancelled')),
    payload TEXT NOT NULL
);

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
    FOREIGN KEY (agent_id, session_id) REFERENCES agents(id, session_id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS agent_executions_one_active_per_agent
    ON agent_executions(agent_id) WHERE status IN ('starting', 'running', 'settling');
CREATE INDEX IF NOT EXISTS agent_executions_recovery ON agent_executions(status, created_at, id);

CREATE TABLE IF NOT EXISTS queued_work_items (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'paused', 'failed', 'interrupted', 'completed', 'cancelled')),
    execution_id TEXT,
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    failure_code TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL,
    UNIQUE (agent_id, sequence),
    UNIQUE (execution_id),
    FOREIGN KEY (agent_id, session_id) REFERENCES agents(id, session_id) ON DELETE RESTRICT,
    FOREIGN KEY (execution_id) REFERENCES agent_executions(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS queued_work_next_by_agent ON queued_work_items(agent_id, status, sequence, id);
CREATE UNIQUE INDEX IF NOT EXISTS queued_work_one_running_per_agent
    ON queued_work_items(agent_id) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS wait_conditions (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    execution_id TEXT NOT NULL REFERENCES agent_executions(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('delivery', 'approval', 'child')),
    mode TEXT NOT NULL CHECK (mode IN ('any', 'all')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'resolved', 'cancelled')),
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS wait_conditions_by_agent ON wait_conditions(agent_id, status, id);

CREATE TABLE IF NOT EXISTS agent_control_requests (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    target_execution_id TEXT REFERENCES agent_executions(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('pause', 'close')),
    status TEXT NOT NULL CHECK (status IN ('requested', 'applied')),
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_control_requests_open ON agent_control_requests(agent_id, status, id);

CREATE TABLE IF NOT EXISTS context_deliveries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    source_artifact_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivering', 'delivered', 'rejected', 'cancelled', 'failed')),
    payload TEXT NOT NULL,
    UNIQUE (target_agent_id, dedupe_key),
    FOREIGN KEY (target_agent_id, session_id) REFERENCES agents(id, session_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS context_deliveries_pending_by_target ON context_deliveries(target_agent_id, status, id);

CREATE TABLE IF NOT EXISTS workspace_write_leases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    workspace_path_snapshot TEXT NOT NULL,
    workspace_revision INTEGER NOT NULL CHECK (workspace_revision > 0),
    owner_agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    capability_grant_id TEXT NOT NULL REFERENCES capability_grants(id) ON DELETE RESTRICT,
    state TEXT NOT NULL CHECK (state IN ('active', 'released')),
    acquired_at TEXT NOT NULL,
    released_at TEXT,
    payload TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS workspace_write_lease_one_active_per_workspace
    ON workspace_write_leases(workspace_id) WHERE state = 'active';

CREATE TABLE IF NOT EXISTS agent_results (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    source_agent_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS briefings (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    source_agent_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected')),
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notes (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    source_agent_id TEXT NOT NULL,
    payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS orchestration_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    work_item_id TEXT NOT NULL DEFAULT '',
    delegation_id TEXT NOT NULL DEFAULT '',
    delivery_id TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS orchestration_events_by_session ON orchestration_events(session_id, occurred_at, id);
