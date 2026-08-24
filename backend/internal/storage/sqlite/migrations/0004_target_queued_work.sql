CREATE TABLE IF NOT EXISTS queued_work_items (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    status TEXT NOT NULL CHECK (status IN (
        'queued', 'running', 'paused', 'failed', 'interrupted', 'completed', 'cancelled'
    )),
    execution_id TEXT REFERENCES agent_executions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    UNIQUE (agent_id, sequence),
    FOREIGN KEY (agent_id, session_id) REFERENCES agents (id, session_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS queued_work_execution_identity
    ON queued_work_items (execution_id)
    WHERE execution_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS queued_work_next_by_agent
    ON queued_work_items (agent_id, status, sequence, id);
