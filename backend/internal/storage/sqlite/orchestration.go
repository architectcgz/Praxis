package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
)

// Target orchestration storage deliberately uses distinct tables while the
// pre-release converter remains to be implemented. Production composition can
// select these repositories without accidentally reading legacy aggregates.

func (s *Store) GetSession(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	var value domain.Session
	return value, s.loadPayload(ctx, "sessions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]domain.Session, error) {
	return listTargetPayloads[domain.Session](
		ctx,
		s,
		`SELECT payload FROM sessions ORDER BY rowid DESC LIMIT ?`,
		[]any{targetLimit(limit)},
		"sessions",
		func(value domain.Session) error { return value.Validate() },
	)
}

func (s *Store) SaveSession(ctx context.Context, value domain.Session) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO sessions (id, workspace_key, payload) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET workspace_key = excluded.workspace_key, payload = excluded.payload`,
		value.ID.String(), value.WorkspaceKey, payload,
	)
}

func (s *Store) GetAgentGroup(ctx context.Context, id domain.AgentGroupID) (domain.AgentGroup, error) {
	var value domain.AgentGroup
	return value, s.loadPayload(ctx, "agent_groups", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentGroup(ctx context.Context, value domain.AgentGroup) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_groups (id, session_id, primary_agent_id, max_concurrent, payload)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id,
		 primary_agent_id = excluded.primary_agent_id, max_concurrent = excluded.max_concurrent,
		 payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.PrimaryAgentID.String(), value.MaxConcurrent, payload,
	)
}

func (s *Store) ListAgentGroupsBySession(
	ctx context.Context,
	sessionID domain.SessionID,
	limit int,
) ([]domain.AgentGroup, error) {
	return listTargetPayloads[domain.AgentGroup](
		ctx,
		s,
		`SELECT payload FROM agent_groups WHERE session_id = ? ORDER BY id LIMIT ?`,
		[]any{sessionID.String(), targetLimit(limit)},
		"agent groups",
		func(value domain.AgentGroup) error { return value.Validate() },
	)
}

func (s *Store) GetAgent(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	var value domain.Agent
	return value, s.loadPayload(ctx, "agents", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgent(ctx context.Context, value domain.Agent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agents (
			id, session_id, group_id, profile, task_packet_id, context_manifest_id,
			capability_grant_id, state, current_execution_id, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, group_id = excluded.group_id, profile = excluded.profile,
			task_packet_id = excluded.task_packet_id, context_manifest_id = excluded.context_manifest_id,
			capability_grant_id = excluded.capability_grant_id, state = excluded.state,
			current_execution_id = excluded.current_execution_id, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.GroupID.String(), string(value.Profile),
		value.TaskPacketID.String(), value.ContextManifestID.String(), value.GrantID.String(), string(value.State),
		value.CurrentExecutionID.String(), payload,
	)
}

func (s *Store) ListAgentsByGroup(
	ctx context.Context,
	groupID domain.AgentGroupID,
	limit int,
) ([]domain.Agent, error) {
	return listTargetPayloads[domain.Agent](
		ctx,
		s,
		`SELECT payload FROM agents WHERE group_id = ? ORDER BY id LIMIT ?`,
		[]any{groupID.String(), targetLimit(limit)},
		"agents",
		func(value domain.Agent) error { return value.Validate() },
	)
}

func (s *Store) ListAgents(ctx context.Context, limit int) ([]domain.Agent, error) {
	return listTargetPayloads[domain.Agent](
		ctx,
		s,
		`SELECT payload FROM agents ORDER BY session_id, group_id, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"agents",
		func(value domain.Agent) error { return value.Validate() },
	)
}

func (s *Store) GetAgentExecution(
	ctx context.Context,
	id domain.AgentExecutionID,
) (domain.AgentExecution, error) {
	var value domain.AgentExecution
	return value, s.loadPayload(ctx, "agent_executions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentExecution(ctx context.Context, value domain.AgentExecution) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO agent_executions (
			id, session_id, agent_id, request_id, work_item_id, reason, status, outcome,
			created_at, started_at, settled_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, agent_id = excluded.agent_id, request_id = excluded.request_id,
			work_item_id = excluded.work_item_id, reason = excluded.reason, status = excluded.status,
			outcome = excluded.outcome, created_at = excluded.created_at, started_at = excluded.started_at,
			settled_at = excluded.settled_at, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.AgentID.String(), value.RequestID.String(),
		value.WorkItemID.String(), string(value.Reason), string(value.Status), string(value.Outcome),
		nullableTimeValue(value.CreatedAt),
		nullableTimeValue(value.StartedAt),
		nullableTimeValue(value.SettledAt),
		payload,
	)
	if err != nil && isConstraintError(err, "agent_executions.agent_id, agent_executions.request_id") {
		return fmt.Errorf("%w: %s", domain.ErrRequestConflict, value.RequestID)
	}
	if err != nil && isConstraintError(err, "agent_executions.agent_id") {
		return fmt.Errorf("%w: %s", domain.ErrAgentExecuting, value.AgentID)
	}
	return err
}

func (s *Store) FindExecutionByRequest(
	ctx context.Context,
	agentID domain.AgentID,
	requestID domain.RequestID,
) (domain.AgentExecution, error) {
	return loadTargetPayload[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE agent_id = ? AND request_id = ?`,
		[]any{agentID.String(), requestID.String()},
		"agent execution request",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) GetActiveExecutionByAgent(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.AgentExecution, error) {
	return loadTargetPayload[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions
		 WHERE agent_id = ? AND status IN ('starting', 'running', 'settling')`,
		[]any{agentID.String()},
		"active agent execution",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) ListAgentExecutionsByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentExecution, error) {
	return listTargetPayloads[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE agent_id = ?
		 ORDER BY created_at DESC, id DESC LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"agent executions",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) CountActiveExecutionsByGroup(ctx context.Context, groupID domain.AgentGroupID) (int, error) {
	var count int
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM agent_executions execution
		 JOIN agents agent ON agent.id = execution.agent_id
		 WHERE agent.group_id = ? AND execution.status IN ('starting', 'running', 'settling')`,
		groupID.String(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active executions for group: %w", err)
	}
	return count, nil
}

func (s *Store) ListRecoverableExecutions(ctx context.Context, limit int) ([]domain.AgentExecution, error) {
	return listTargetPayloads[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions
		 WHERE status IN ('starting', 'running', 'settling') ORDER BY created_at, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"recoverable agent executions",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) ListStartingExecutions(ctx context.Context, limit int) ([]domain.AgentExecution, error) {
	return listTargetPayloads[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE status = 'starting' ORDER BY created_at, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"starting agent executions",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) GetQueuedWork(ctx context.Context, id domain.WorkItemID) (domain.QueuedWork, error) {
	return loadTargetPayload[domain.QueuedWork](
		ctx,
		s,
		`SELECT payload FROM queued_work_items WHERE id = ?`,
		[]any{id.String()},
		"queued work",
		func(value domain.QueuedWork) error { return value.Validate() },
	)
}

func (s *Store) SaveQueuedWork(ctx context.Context, value domain.QueuedWork) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO queued_work_items (
			id, session_id, agent_id, sequence, status, execution_id, created_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, agent_id = excluded.agent_id, sequence = excluded.sequence,
			status = excluded.status, execution_id = excluded.execution_id,
			created_at = excluded.created_at, payload = excluded.payload`,
		value.ID.String(),
		value.SessionID.String(),
		value.AgentID.String(),
		value.Sequence,
		string(value.Status),
		nullableID(value.ExecutionID),
		nullableTimeValue(value.CreatedAt),
		payload,
	)
	if err != nil && isConstraintError(err, "queued_work_items.agent_id, queued_work_items.sequence") {
		return fmt.Errorf("%w: queued work sequence for agent %s", domain.ErrRequestConflict, value.AgentID)
	}
	if err != nil && isConstraintError(err, "queued_work_items.execution_id") {
		return fmt.Errorf("%w: queued work execution %s", domain.ErrRequestConflict, value.ExecutionID)
	}
	return err
}

func (s *Store) NextQueuedWorkSequence(ctx context.Context, agentID domain.AgentID) (uint64, error) {
	var sequence uint64
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM queued_work_items WHERE agent_id = ?`,
		agentID.String(),
	).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("allocate queued work sequence: %w", err)
	}
	return sequence, nil
}

func (s *Store) FindNextPendingQueuedWork(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.QueuedWork, error) {
	return loadTargetPayload[domain.QueuedWork](
		ctx,
		s,
		`SELECT payload FROM queued_work_items
		 WHERE agent_id = ? AND status = 'queued' ORDER BY sequence, id LIMIT 1`,
		[]any{agentID.String()},
		"next queued work",
		func(value domain.QueuedWork) error { return value.Validate() },
	)
}

func (s *Store) ListRunningQueuedWork(ctx context.Context, limit int) ([]domain.QueuedWork, error) {
	return listTargetPayloads[domain.QueuedWork](
		ctx,
		s,
		`SELECT payload FROM queued_work_items WHERE status = 'running' ORDER BY created_at, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"running queued work",
		func(value domain.QueuedWork) error { return value.Validate() },
	)
}

func (s *Store) GetWaitCondition(
	ctx context.Context,
	id domain.WaitConditionID,
) (domain.WaitCondition, error) {
	var value domain.WaitCondition
	return value, s.loadPayload(ctx, "wait_conditions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveWaitCondition(ctx context.Context, value domain.WaitCondition) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO wait_conditions (id, agent_id, execution_id, kind, status, payload)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id,
		 execution_id = excluded.execution_id, kind = excluded.kind, status = excluded.status,
		 payload = excluded.payload`,
		value.ID.String(),
		value.AgentID.String(),
		value.ExecutionID.String(),
		string(value.Kind),
		string(value.Status),
		payload,
	)
}

func (s *Store) ListUnresolvedWaitConditionsByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.WaitCondition, error) {
	return listTargetPayloads[domain.WaitCondition](
		ctx,
		s,
		`SELECT payload FROM wait_conditions WHERE agent_id = ? AND status = 'pending' ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"unresolved wait conditions",
		func(value domain.WaitCondition) error { return value.Validate() },
	)
}

func (s *Store) GetAgentControlRequest(
	ctx context.Context,
	id domain.AgentControlRequestID,
) (domain.AgentControlRequest, error) {
	var value domain.AgentControlRequest
	return value, s.loadPayload(
		ctx,
		"agent_control_requests",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveAgentControlRequest(ctx context.Context, value domain.AgentControlRequest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_control_requests (id, agent_id, target_execution_id, kind, status, payload)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id,
		 target_execution_id = excluded.target_execution_id, kind = excluded.kind,
		 status = excluded.status, payload = excluded.payload`,
		value.ID.String(), value.AgentID.String(), nullableID(value.TargetExecutionID), string(value.Kind),
		string(value.Status), payload,
	)
}

func (s *Store) ListOpenAgentControlRequestsByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentControlRequest, error) {
	return listTargetPayloads[domain.AgentControlRequest](
		ctx,
		s,
		`SELECT payload FROM agent_control_requests
		 WHERE agent_id = ? AND status = 'requested' ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"open agent control requests",
		func(value domain.AgentControlRequest) error { return value.Validate() },
	)
}

func (s *Store) GetContextDelivery(ctx context.Context, id domain.DeliveryID) (domain.ContextDelivery, error) {
	var value domain.ContextDelivery
	return value, s.loadPayload(
		ctx,
		"context_deliveries",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveContextDelivery(ctx context.Context, value domain.ContextDelivery) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO context_deliveries (
			id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id,
		source_artifact_id = excluded.source_artifact_id, target_agent_id = excluded.target_agent_id,
		dedupe_key = excluded.dedupe_key, status = excluded.status, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.SourceArtifactID, value.TargetAgentID.String(),
		value.DedupeKey, string(value.Status), payload,
	)
}

func (s *Store) ListPendingContextDeliveriesByTarget(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.ContextDelivery, error) {
	return listTargetPayloads[domain.ContextDelivery](
		ctx,
		s,
		`SELECT payload FROM context_deliveries
		 WHERE target_agent_id = ? AND status IN ('pending', 'delivering') ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"pending context deliveries",
		func(value domain.ContextDelivery) error { return value.Validate() },
	)
}

func (s *Store) ListInFlightContextDeliveries(ctx context.Context, limit int) ([]domain.ContextDelivery, error) {
	return listTargetPayloads[domain.ContextDelivery](
		ctx,
		s,
		`SELECT payload FROM context_deliveries
		 WHERE status IN ('pending', 'delivering') ORDER BY id LIMIT ?`,
		[]any{targetLimit(limit)},
		"in-flight context deliveries",
		func(value domain.ContextDelivery) error { return value.Validate() },
	)
}

func (s *Store) HasDeliveringContextDeliveryByTarget(ctx context.Context, agentID domain.AgentID) (bool, error) {
	var exists bool
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM context_deliveries WHERE target_agent_id = ? AND status = 'delivering'
		)`,
		agentID.String(),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect delivering context delivery: %w", err)
	}
	return exists, nil
}

func targetLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func loadTargetPayload[T any](
	ctx context.Context,
	store *Store,
	query string,
	args []any,
	name string,
	validate func(T) error,
) (T, error) {
	var zero T
	row := executorFromContext(ctx, store.db).QueryRowContext(ctx, query, args...)
	var payload []byte
	if err := row.Scan(&payload); errors.Is(err, sql.ErrNoRows) {
		return zero, domain.ErrNotFound
	} else if err != nil {
		return zero, fmt.Errorf("read %s: %w", name, err)
	}
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return zero, fmt.Errorf("decode stored %s: %w", name, err)
	}
	if err := validate(value); err != nil {
		return zero, fmt.Errorf("validate stored %s: %w", name, err)
	}
	return value, nil
}

func listTargetPayloads[T any](
	ctx context.Context,
	store *Store,
	query string,
	args []any,
	name string,
	validate func(T) error,
) ([]T, error) {
	rows, err := executorFromContext(ctx, store.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", name, err)
	}
	defer rows.Close()
	values := make([]T, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}
		var value T
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode stored %s: %w", name, err)
		}
		if err := validate(value); err != nil {
			return nil, fmt.Errorf("validate stored %s: %w", name, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", name, err)
	}
	return values, nil
}

type TargetRepositories struct {
	Sessions   SessionRepository
	Groups     AgentGroupRepository
	Agents     AgentRepository
	Executions AgentExecutionRepository
	QueuedWork QueuedWorkRepository
	Waits      WaitConditionRepository
	Controls   AgentControlRequestRepository
	Deliveries ContextDeliveryRepository
}

func (s *Store) TargetRepositories() TargetRepositories {
	return TargetRepositories{
		Sessions:   SessionRepository{s},
		Groups:     AgentGroupRepository{s},
		Agents:     AgentRepository{s},
		Executions: AgentExecutionRepository{s},
		QueuedWork: QueuedWorkRepository{s},
		Waits:      WaitConditionRepository{s},
		Controls:   AgentControlRequestRepository{s},
		Deliveries: ContextDeliveryRepository{s},
	}
}

type SessionRepository struct{ store *Store }

func (r SessionRepository) Get(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	return r.store.GetSession(ctx, id)
}

func (r SessionRepository) Save(ctx context.Context, value domain.Session) error {
	return r.store.SaveSession(ctx, value)
}

func (r SessionRepository) List(ctx context.Context, limit int) ([]domain.Session, error) {
	return r.store.ListSessions(ctx, limit)
}

type AgentGroupRepository struct{ store *Store }

func (r AgentGroupRepository) Get(ctx context.Context, id domain.AgentGroupID) (domain.AgentGroup, error) {
	return r.store.GetAgentGroup(ctx, id)
}

func (r AgentGroupRepository) Save(ctx context.Context, value domain.AgentGroup) error {
	return r.store.SaveAgentGroup(ctx, value)
}

func (r AgentGroupRepository) ListBySession(
	ctx context.Context,
	sessionID domain.SessionID,
	limit int,
) ([]domain.AgentGroup, error) {
	return r.store.ListAgentGroupsBySession(ctx, sessionID, limit)
}

type AgentRepository struct{ store *Store }

func (r AgentRepository) Get(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	return r.store.GetAgent(ctx, id)
}

func (r AgentRepository) Save(ctx context.Context, value domain.Agent) error {
	return r.store.SaveAgent(ctx, value)
}

func (r AgentRepository) ListByGroup(
	ctx context.Context,
	groupID domain.AgentGroupID,
	limit int,
) ([]domain.Agent, error) {
	return r.store.ListAgentsByGroup(ctx, groupID, limit)
}

func (r AgentRepository) ListAll(ctx context.Context, limit int) ([]domain.Agent, error) {
	return r.store.ListAgents(ctx, limit)
}

type AgentExecutionRepository struct{ store *Store }

func (r AgentExecutionRepository) Get(
	ctx context.Context,
	id domain.AgentExecutionID,
) (domain.AgentExecution, error) {
	return r.store.GetAgentExecution(ctx, id)
}

func (r AgentExecutionRepository) Save(ctx context.Context, value domain.AgentExecution) error {
	return r.store.SaveAgentExecution(ctx, value)
}

func (r AgentExecutionRepository) FindByRequest(
	ctx context.Context,
	agentID domain.AgentID,
	requestID domain.RequestID,
) (domain.AgentExecution, error) {
	return r.store.FindExecutionByRequest(ctx, agentID, requestID)
}

func (r AgentExecutionRepository) GetActiveByAgent(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.AgentExecution, error) {
	return r.store.GetActiveExecutionByAgent(ctx, agentID)
}

func (r AgentExecutionRepository) ListByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentExecution, error) {
	return r.store.ListAgentExecutionsByAgent(ctx, agentID, limit)
}

func (r AgentExecutionRepository) CountActiveByGroup(ctx context.Context, groupID domain.AgentGroupID) (int, error) {
	return r.store.CountActiveExecutionsByGroup(ctx, groupID)
}

func (r AgentExecutionRepository) ListRecoverable(
	ctx context.Context,
	limit int,
) ([]domain.AgentExecution, error) {
	return r.store.ListRecoverableExecutions(ctx, limit)
}

func (r AgentExecutionRepository) ListStarting(
	ctx context.Context,
	limit int,
) ([]domain.AgentExecution, error) {
	return r.store.ListStartingExecutions(ctx, limit)
}

type QueuedWorkRepository struct{ store *Store }

func (r QueuedWorkRepository) Get(ctx context.Context, id domain.WorkItemID) (domain.QueuedWork, error) {
	return r.store.GetQueuedWork(ctx, id)
}

func (r QueuedWorkRepository) Save(ctx context.Context, value domain.QueuedWork) error {
	return r.store.SaveQueuedWork(ctx, value)
}

func (r QueuedWorkRepository) NextSequence(ctx context.Context, agentID domain.AgentID) (uint64, error) {
	return r.store.NextQueuedWorkSequence(ctx, agentID)
}

func (r QueuedWorkRepository) FindNextPendingByAgent(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.QueuedWork, error) {
	return r.store.FindNextPendingQueuedWork(ctx, agentID)
}

func (r QueuedWorkRepository) ListRunning(ctx context.Context, limit int) ([]domain.QueuedWork, error) {
	return r.store.ListRunningQueuedWork(ctx, limit)
}

type WaitConditionRepository struct{ store *Store }

func (r WaitConditionRepository) Get(
	ctx context.Context,
	id domain.WaitConditionID,
) (domain.WaitCondition, error) {
	return r.store.GetWaitCondition(ctx, id)
}

func (r WaitConditionRepository) Save(ctx context.Context, value domain.WaitCondition) error {
	return r.store.SaveWaitCondition(ctx, value)
}

func (r WaitConditionRepository) ListUnresolvedByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.WaitCondition, error) {
	return r.store.ListUnresolvedWaitConditionsByAgent(ctx, agentID, limit)
}

type AgentControlRequestRepository struct{ store *Store }

func (r AgentControlRequestRepository) Get(
	ctx context.Context,
	id domain.AgentControlRequestID,
) (domain.AgentControlRequest, error) {
	return r.store.GetAgentControlRequest(ctx, id)
}

func (r AgentControlRequestRepository) Save(ctx context.Context, value domain.AgentControlRequest) error {
	return r.store.SaveAgentControlRequest(ctx, value)
}

func (r AgentControlRequestRepository) ListOpenByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentControlRequest, error) {
	return r.store.ListOpenAgentControlRequestsByAgent(ctx, agentID, limit)
}

type ContextDeliveryRepository struct{ store *Store }

func (r ContextDeliveryRepository) Get(
	ctx context.Context,
	id domain.DeliveryID,
) (domain.ContextDelivery, error) {
	return r.store.GetContextDelivery(ctx, id)
}

func (r ContextDeliveryRepository) Save(ctx context.Context, value domain.ContextDelivery) error {
	return r.store.SaveContextDelivery(ctx, value)
}

func (r ContextDeliveryRepository) ListPendingByTarget(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.ContextDelivery, error) {
	return r.store.ListPendingContextDeliveriesByTarget(ctx, agentID, limit)
}

func (r ContextDeliveryRepository) ListInFlight(
	ctx context.Context,
	limit int,
) ([]domain.ContextDelivery, error) {
	return r.store.ListInFlightContextDeliveries(ctx, limit)
}

func (r ContextDeliveryRepository) HasDeliveringByTarget(ctx context.Context, agentID domain.AgentID) (bool, error) {
	return r.store.HasDeliveringContextDeliveryByTarget(ctx, agentID)
}

var (
	_ persistence.SessionRepository             = SessionRepository{}
	_ persistence.AgentGroupRepository          = AgentGroupRepository{}
	_ persistence.AgentRepository               = AgentRepository{}
	_ persistence.AgentExecutionRepository      = AgentExecutionRepository{}
	_ persistence.QueuedWorkRepository          = QueuedWorkRepository{}
	_ persistence.WaitConditionRepository       = WaitConditionRepository{}
	_ persistence.AgentControlRequestRepository = AgentControlRequestRepository{}
	_ persistence.ContextDeliveryRepository     = ContextDeliveryRepository{}
)
