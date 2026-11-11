package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

const historySchema = `
CREATE TABLE IF NOT EXISTS history_logs (
 session_id TEXT NOT NULL, branch_id TEXT NOT NULL, agent_id TEXT NOT NULL, progress INTEGER NOT NULL,
 PRIMARY KEY (session_id, branch_id, agent_id),
 FOREIGN KEY (session_id, branch_id) REFERENCES session_branches(session_id, id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS history_entries (
 session_id TEXT NOT NULL, id TEXT NOT NULL, parent_id TEXT NOT NULL, branch_id TEXT NOT NULL,
 agent_id TEXT NOT NULL, run_id TEXT NOT NULL, metadata TEXT NOT NULL, offset INTEGER NOT NULL, length INTEGER NOT NULL,
 PRIMARY KEY (session_id, id),
 FOREIGN KEY (session_id, branch_id) REFERENCES session_branches(session_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS history_entry_head ON history_entries(session_id, branch_id, agent_id, offset DESC);
CREATE INDEX IF NOT EXISTS history_entry_run ON history_entries(session_id, run_id, offset);
CREATE TABLE IF NOT EXISTS history_runs (
 session_id TEXT NOT NULL, id TEXT NOT NULL, branch_id TEXT NOT NULL, agent_id TEXT NOT NULL,
 record TEXT NOT NULL, end_entry_id TEXT NOT NULL, offset INTEGER NOT NULL, length INTEGER NOT NULL,
 PRIMARY KEY (session_id, id),
 FOREIGN KEY (session_id, branch_id) REFERENCES session_branches(session_id, id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS history_requests (
 session_id TEXT NOT NULL, id TEXT NOT NULL, branch_id TEXT NOT NULL, run_id TEXT NOT NULL, turn_id TEXT NOT NULL, record TEXT NOT NULL,
 PRIMARY KEY (session_id, id),
 FOREIGN KEY (session_id, branch_id) REFERENCES session_branches(session_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS history_request_turn ON history_requests(session_id, turn_id);
CREATE TABLE IF NOT EXISTS history_tools (
 session_id TEXT NOT NULL, run_id TEXT NOT NULL, call_id TEXT NOT NULL, branch_id TEXT NOT NULL,
 entry_id TEXT NOT NULL, completed INTEGER NOT NULL,
 PRIMARY KEY (session_id, run_id, call_id),
 FOREIGN KEY (session_id, branch_id) REFERENCES session_branches(session_id, id) ON DELETE CASCADE
);`

func (l SessionLog) Progress(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID) (int64, error) {
	var progress int64
	err := l.database.QueryRowContext(ctx, `SELECT progress FROM history_logs WHERE session_id=? AND branch_id=? AND agent_id=?`, l.sessionID, branchID, agentID).Scan(&progress)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return progress, err
}

// Index 不在事务内读取或解码日志；所有投影与进度作为一个提交单元。
func (l SessionLog) Index(ctx context.Context, update repository.HistoryUpdate) error {
	position := update.Position
	var nodeJSON, runJSON, requestJSON []byte
	var err error
	if update.Entry != nil {
		nodeJSON, err = json.Marshal(update.Entry)
		if err != nil {
			return err
		}
	}
	if update.Run != nil {
		runJSON, err = json.Marshal(update.Run)
		if err != nil {
			return err
		}
	}
	if update.Request != nil {
		requestJSON, err = json.Marshal(update.Request)
		if err != nil {
			return err
		}
	}
	tx, err := l.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var progress int64
	readErr := tx.QueryRowContext(ctx, `SELECT progress FROM history_logs WHERE session_id=? AND branch_id=? AND agent_id=?`, l.sessionID, position.BranchID, position.AgentID).Scan(&progress)
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return readErr
	}
	if progress != position.Offset {
		return fmt.Errorf("history index is not contiguous: indexed %d, record offset %d", progress, position.Offset)
	}
	if update.Entry != nil {
		node := update.Entry
		_, err = tx.ExecContext(ctx, `INSERT INTO history_entries VALUES (?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(session_id,id) DO NOTHING`, l.sessionID, node.ID, node.ParentID, position.BranchID, position.AgentID, node.RunID, string(nodeJSON), position.Offset, position.Length)
		if err != nil {
			return fmt.Errorf("index entry %s: %w", node.ID, err)
		}
		for _, callID := range update.Calls {
			if _, err := tx.ExecContext(ctx, `INSERT INTO history_tools VALUES (?,?,?,?,?,0) ON CONFLICT DO NOTHING`, l.sessionID, node.RunID, callID, position.BranchID, node.ID); err != nil {
				return err
			}
		}
		for _, callID := range update.Results {
			if _, err := tx.ExecContext(ctx, `UPDATE history_tools SET completed=1 WHERE session_id=? AND run_id=? AND call_id=?`, l.sessionID, node.RunID, callID); err != nil {
				return err
			}
		}
	}
	if update.Run != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO history_runs VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT(session_id,id) DO UPDATE SET record=excluded.record,
		 end_entry_id=CASE WHEN excluded.end_entry_id<>'' THEN excluded.end_entry_id ELSE history_runs.end_entry_id END,
		 offset=excluded.offset,length=excluded.length`, l.sessionID, update.Run.ID, position.BranchID, position.AgentID, string(runJSON), update.EndEntryID, position.Offset, position.Length)
		if err != nil {
			return fmt.Errorf("index run %s: %w", update.Run.ID, err)
		}
	}
	if update.Request != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO history_requests VALUES (?,?,?,?,?,?)
		 ON CONFLICT(session_id,id) DO UPDATE SET record=excluded.record`, l.sessionID, update.Request.ID, position.BranchID, update.Request.RunID, update.Request.TurnID, string(requestJSON))
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO history_logs VALUES (?,?,?,?)
	 ON CONFLICT(session_id,branch_id,agent_id) DO UPDATE SET progress=excluded.progress`, l.sessionID, position.BranchID, position.AgentID, position.Offset+position.Length)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (l SessionLog) Node(ctx context.Context, id string) (sessionentry.Node, error) {
	values, err := l.EntryLocations(ctx, []string{id})
	if err != nil {
		return sessionentry.Node{}, err
	}
	if len(values) == 0 {
		return sessionentry.Node{}, errorcode.ErrNotFound
	}
	return values[0].Node, nil
}

func (l SessionLog) PathNodes(ctx context.Context, head string) ([]sessionentry.Node, error) {
	rows, err := l.database.QueryContext(ctx, `WITH RECURSIVE path(id,parent_id,metadata,depth) AS (
	 SELECT id,parent_id,metadata,0 FROM history_entries WHERE session_id=? AND id=?
	 UNION ALL SELECT e.id,e.parent_id,e.metadata,p.depth+1 FROM history_entries e JOIN path p ON e.id=p.parent_id WHERE e.session_id=?
	) SELECT metadata FROM path ORDER BY depth DESC`, l.sessionID, head, l.sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (l SessionLog) TreeNodes(ctx context.Context) ([]sessionentry.Node, error) {
	rows, err := l.database.QueryContext(ctx, `SELECT metadata FROM history_entries WHERE session_id=? AND agent_id=? ORDER BY offset,id`, l.sessionID, l.mainAgent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func scanNodes(rows *sql.Rows) ([]sessionentry.Node, error) {
	nodes := make([]sessionentry.Node, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		node, err := decodeNode(raw)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func decodeNode(raw string) (sessionentry.Node, error) {
	var node sessionentry.Node
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return node, err
	}
	if node.ID == "" || node.BranchID == "" || node.AgentID == "" || node.CreatedAt.IsZero() {
		return node, errors.New("invalid indexed entry metadata")
	}
	return node, nil
}

// EntryLocations 一次取得整页或上下文范围的位置，不逐节点往返数据库。
func (l SessionLog) EntryLocations(ctx context.Context, ids []string) ([]repository.IndexedEntry, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := l.database.QueryContext(ctx, `SELECT e.metadata,e.branch_id,e.agent_id,e.offset,e.length
	 FROM json_each(?) wanted JOIN history_entries e ON e.id=wanted.value AND e.session_id=? ORDER BY wanted.key`, string(encoded), l.sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]repository.IndexedEntry, 0, len(ids))
	for rows.Next() {
		var value repository.IndexedEntry
		var raw string
		if err := rows.Scan(&raw, &value.Position.BranchID, &value.Position.AgentID, &value.Position.Offset, &value.Position.Length); err != nil {
			return nil, err
		}
		value.Node, err = decodeNode(raw)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (l SessionLog) Runs(ctx context.Context) ([]repository.RunLocation, error) {
	rows, err := l.database.QueryContext(ctx, `SELECT record,end_entry_id,branch_id,agent_id,offset,length FROM history_runs WHERE session_id=?`, l.sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]repository.RunLocation, 0)
	for rows.Next() {
		value, err := scanRunLocation(rows, l.sessionID)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (l SessionLog) Run(ctx context.Context, id identity.RunID) (repository.RunLocation, error) {
	return scanRunLocation(l.database.QueryRowContext(ctx, `SELECT record,end_entry_id,branch_id,agent_id,offset,length FROM history_runs WHERE session_id=? AND id=?`, l.sessionID, id), l.sessionID)
}

func scanRunLocation(row interface{ Scan(...any) error }, sessionID identity.SessionID) (repository.RunLocation, error) {
	var value repository.RunLocation
	var raw string
	if err := row.Scan(&raw, &value.EndEntryID, &value.Position.BranchID, &value.Position.AgentID, &value.Position.Offset, &value.Position.Length); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return value, errorcode.ErrNotFound
		}
		return value, err
	}
	if err := json.Unmarshal([]byte(raw), &value.Record); err != nil {
		return value, err
	}
	if value.Record.ID == "" || value.Record.SessionID != sessionID {
		return value, errors.New("invalid indexed run identity")
	}
	switch value.Record.Status {
	case agentrun.RunStarting, agentrun.RunRunning, agentrun.RunEnding, agentrun.RunEnded:
	default:
		return value, errors.New("invalid indexed run status")
	}
	if value.Record.Status == agentrun.RunEnded {
		switch value.Record.Outcome {
		case agentrun.RunCompleted, agentrun.RunYielded, agentrun.RunFailed, agentrun.RunInterrupted:
		default: return value, errors.New("invalid indexed run outcome")
		}
	}
	return value, nil
}

func (l SessionLog) AgentHeads(ctx context.Context) ([]repository.AgentHead, error) {
	rows, err := l.database.QueryContext(ctx, `SELECT agent_id,branch_id,id FROM (
	 SELECT agent_id,branch_id,id,ROW_NUMBER() OVER (PARTITION BY agent_id ORDER BY offset DESC) AS latest
	 FROM history_entries WHERE session_id=? AND agent_id<>?
	) WHERE latest=1`, l.sessionID, l.mainAgent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []repository.AgentHead
	for rows.Next() {
		var value repository.AgentHead
		if err := rows.Scan(&value.AgentID, &value.BranchID, &value.EntryID); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (l SessionLog) RecoveryRuns(ctx context.Context) ([]identity.RunID, error) {
	rows, err := l.database.QueryContext(ctx, `SELECT id FROM history_runs r WHERE session_id=? AND
	 (end_entry_id='' OR EXISTS (SELECT 1 FROM history_tools t WHERE t.session_id=r.session_id AND t.run_id=r.id AND t.completed=0))`, l.sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []identity.RunID
	for rows.Next() {
		var id identity.RunID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (l SessionLog) RunEntries(ctx context.Context, id identity.RunID) ([]string, error) {
	rows, err := l.database.QueryContext(ctx, `SELECT id FROM history_entries WHERE session_id=? AND run_id=? ORDER BY offset`, l.sessionID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (l SessionLog) Requests(ctx context.Context, turnIDs []identity.TurnID) ([]agentrun.ModelRequestRecord, error) {
	encoded, err := json.Marshal(turnIDs)
	if err != nil {
		return nil, err
	}
	rows, err := l.database.QueryContext(ctx, `SELECT record FROM history_requests WHERE session_id=? AND turn_id IN (SELECT value FROM json_each(?))`, l.sessionID, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]agentrun.ModelRequestRecord, 0)
	for rows.Next() {
		var raw string
		var value agentrun.ModelRequestRecord
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		if err := value.Validate(); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
