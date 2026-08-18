package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
)

const workItemColumns = `id, task_session_id, agent_thread_id, sequence, prompt,
    task_packet_id, context_manifest_id, capability_grant_id, sandbox_mode,
    approval_mode, execution_revision, status, agent_run_id, created_at,
    started_at, finished_at, failure_code`

// Get loads one durable work item by ID.
func (s *Store) Get(ctx context.Context, id domain.WorkItemID) (domain.QueuedWorkItem, error) {
	row := executorFromContext(ctx, s.db).QueryRowContext(ctx, `SELECT `+workItemColumns+` FROM work_queue_items WHERE id = ?`, id.String())
	item, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.QueuedWorkItem{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.QueuedWorkItem{}, fmt.Errorf("get work item: %w", err)
	}
	return item, nil
}

// ListByThread returns work items in FIFO order, optionally filtered by status.
func (s *Store) ListByThread(ctx context.Context, threadID domain.AgentThreadID, status domain.WorkItemStatus, limit int) ([]domain.QueuedWorkItem, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT ` + workItemColumns + ` FROM work_queue_items WHERE agent_thread_id = ?`
	args := []any{threadID.String()}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY sequence, id LIMIT ?`
	args = append(args, limit)
	rows, err := executorFromContext(ctx, s.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	defer rows.Close()
	items := make([]domain.QueuedWorkItem, 0)
	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan work item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work items: %w", err)
	}
	return items, nil
}

// NextSequence returns the next FIFO sequence for a thread.
func (s *Store) NextSequence(ctx context.Context, threadID domain.AgentThreadID) (uint64, error) {
	var next int64
	err := executorFromContext(ctx, s.db).QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM work_queue_items
		WHERE agent_thread_id = ?`, threadID.String()).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("allocate work item sequence: %w", err)
	}
	if next <= 0 {
		return 0, errors.New("work item sequence overflow")
	}
	return uint64(next), nil
}

// Enqueue persists a validated queued item without claiming a run or lease.
func (s *Store) Enqueue(ctx context.Context, item domain.QueuedWorkItem) error {
	if err := item.Validate(); err != nil {
		return err
	}
	_, err := executorFromContext(ctx, s.db).ExecContext(ctx, `
		INSERT INTO work_queue_items (
			id, task_session_id, agent_thread_id, sequence, prompt,
			task_packet_id, context_manifest_id, capability_grant_id,
			sandbox_mode, approval_mode, execution_revision, status, agent_run_id,
			created_at, started_at, finished_at, failure_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID.String(), item.TaskSessionID.String(), item.AgentThreadID.String(), item.Sequence, item.Prompt,
		item.TaskPacketID.String(), item.ContextManifestID.String(), item.CapabilityGrantID.String(),
		string(item.Execution.SandboxMode), string(item.Execution.ApprovalMode), item.Execution.Revision,
		string(item.Status), nullableID(item.AgentRunID), formatTime(item.CreatedAt), formatTime(item.StartedAt),
		formatTime(item.FinishedAt), item.FailureCode,
	)
	if err != nil {
		return fmt.Errorf("enqueue work item: %w", err)
	}
	return nil
}

// Save persists a domain transition that has already been validated by core.
func (s *Store) Save(ctx context.Context, item domain.QueuedWorkItem) error {
	if err := item.Validate(); err != nil {
		return err
	}
	result, err := executorFromContext(ctx, s.db).ExecContext(ctx, `
		UPDATE work_queue_items
		SET status = ?, agent_run_id = ?, started_at = ?, finished_at = ?, failure_code = ?
		WHERE id = ?`,
		string(item.Status), nullableID(item.AgentRunID), formatTime(item.StartedAt), formatTime(item.FinishedAt), item.FailureCode, item.ID.String())
	if err != nil {
		return fmt.Errorf("save work item: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check saved work item: %w", err)
	} else if affected != 1 {
		return domain.ErrNotFound
	}
	return nil
}

// ClaimNext atomically claims the earliest queued item for a thread.
func (s *Store) ClaimNext(ctx context.Context, threadID domain.AgentThreadID, runID domain.AgentRunID, at time.Time) (domain.QueuedWorkItem, error) {
	if runID == "" {
		return domain.QueuedWorkItem{}, errors.New("work item run id is required")
	}
	if at.IsZero() {
		return domain.QueuedWorkItem{}, errors.New("work item claim time is required")
	}
	var item domain.QueuedWorkItem
	err := s.withValueTx(ctx, func(txCtx context.Context, _ *sql.Tx) error {
		executor := executorFromContext(txCtx, s.db)
		row := executor.QueryRowContext(txCtx, `
			UPDATE work_queue_items
			SET status = ?, agent_run_id = ?, started_at = ?, finished_at = NULL, failure_code = ''
			WHERE id = (
				SELECT id
				FROM work_queue_items
				WHERE agent_thread_id = ?
				  AND status = ?
				  AND NOT EXISTS (
					  SELECT 1 FROM work_queue_items
					  WHERE agent_thread_id = ? AND status = ?
				  )
				ORDER BY sequence, id
				LIMIT 1
			)
			AND status = ?
			RETURNING `+workItemColumns,
			string(domain.WorkItemRunning), runID.String(), formatTime(at),
			threadID.String(), string(domain.WorkItemQueued), threadID.String(), string(domain.WorkItemRunning), string(domain.WorkItemQueued))
		var err error
		item, err = scanWorkItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			return s.claimEmptyError(txCtx, threadID)
		}
		if err != nil {
			return fmt.Errorf("claim next work item: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.QueuedWorkItem{}, err
	}
	return item, nil
}

// CancelQueued cancels an item only while it is still waiting for a run.
func (s *Store) CancelQueued(ctx context.Context, id domain.WorkItemID, at time.Time) (domain.QueuedWorkItem, error) {
	var item domain.QueuedWorkItem
	err := s.withValueTx(ctx, func(txCtx context.Context, _ *sql.Tx) error {
		var err error
		item, err = s.Get(txCtx, id)
		if err != nil {
			return err
		}
		if _, err := item.Cancel(at); err != nil {
			return err
		}
		if err := s.Save(txCtx, item); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.QueuedWorkItem{}, err
	}
	return item, nil
}

// ReconcileRunning marks active items interrupted without replaying side effects.
func (s *Store) ReconcileRunning(ctx context.Context, at time.Time) ([]domain.QueuedWorkItem, error) {
	var reconciled []domain.QueuedWorkItem
	err := s.withValueTx(ctx, func(txCtx context.Context, _ *sql.Tx) error {
		rows, err := executorFromContext(txCtx, s.db).QueryContext(txCtx, `
			SELECT `+workItemColumns+` FROM work_queue_items WHERE status = ? ORDER BY agent_thread_id, sequence`, string(domain.WorkItemRunning))
		if err != nil {
			return fmt.Errorf("find running work items: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanWorkItem(rows)
			if err != nil {
				return fmt.Errorf("scan running work item: %w", err)
			}
			if _, err := item.Settle(item.AgentRunID, domain.RunInterrupted, at, "runtime_interrupted"); err != nil {
				return err
			}
			if err := s.Save(txCtx, item); err != nil {
				return err
			}
			reconciled = append(reconciled, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate running work items: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reconciled, nil
}

func (s *Store) claimEmptyError(ctx context.Context, threadID domain.AgentThreadID) error {
	var active int
	err := executorFromContext(ctx, s.db).QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM work_queue_items WHERE agent_thread_id = ? AND status = ?
		)`, threadID.String(), string(domain.WorkItemRunning)).Scan(&active)
	if err != nil {
		return fmt.Errorf("inspect work queue claim conflict: %w", err)
	}
	if active != 0 {
		return domain.ErrWorkItemActive
	}
	return domain.ErrWorkQueueEmpty
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWorkItem(scanner rowScanner) (domain.QueuedWorkItem, error) {
	var (
		id, sessionID, threadID, prompt     string
		sequence                            int64
		packetID, manifestID, grantID       string
		sandboxMode, approvalMode, revision string
		status, createdAt, failureCode      string
		agentRunID, startedAt, finishedAt   sql.NullString
	)
	if err := scanner.Scan(&id, &sessionID, &threadID, &sequence, &prompt, &packetID, &manifestID, &grantID, &sandboxMode, &approvalMode, &revision, &status, &agentRunID, &createdAt, &startedAt, &finishedAt, &failureCode); err != nil {
		return domain.QueuedWorkItem{}, err
	}
	if sequence <= 0 {
		return domain.QueuedWorkItem{}, errors.New("stored work item sequence is invalid")
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return domain.QueuedWorkItem{}, fmt.Errorf("parse work item createdAt: %w", err)
	}
	started, err := parseNullableTime(startedAt)
	if err != nil {
		return domain.QueuedWorkItem{}, fmt.Errorf("parse work item startedAt: %w", err)
	}
	finished, err := parseNullableTime(finishedAt)
	if err != nil {
		return domain.QueuedWorkItem{}, fmt.Errorf("parse work item finishedAt: %w", err)
	}
	execution, err := domain.NewRuntimeExecutionSnapshot(domain.SandboxMode(sandboxMode), domain.ApprovalMode(approvalMode), revision)
	if err != nil {
		return domain.QueuedWorkItem{}, fmt.Errorf("parse work item execution: %w", err)
	}
	item := domain.QueuedWorkItem{
		ID:                domain.WorkItemID(id),
		TaskSessionID:     domain.TaskSessionID(sessionID),
		AgentThreadID:     domain.AgentThreadID(threadID),
		Sequence:          uint64(sequence),
		Prompt:            prompt,
		TaskPacketID:      domain.TaskPacketID(packetID),
		ContextManifestID: domain.ContextManifestID(manifestID),
		CapabilityGrantID: domain.CapabilityGrantID(grantID),
		Execution:         execution,
		Status:            domain.WorkItemStatus(status),
		CreatedAt:         created,
		StartedAt:         started,
		FinishedAt:        finished,
		FailureCode:       failureCode,
	}
	if agentRunID.Valid {
		item.AgentRunID = domain.AgentRunID(agentRunID.String)
	}
	if err := item.Validate(); err != nil {
		return domain.QueuedWorkItem{}, fmt.Errorf("validate stored work item: %w", err)
	}
	return item, nil
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("timestamp is empty")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func parseNullableTime(value sql.NullString) (time.Time, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}
	return parseTime(value.String)
}

var _ persistence.WorkQueueRepository = (*Store)(nil)
