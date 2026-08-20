package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
)

// Product repositories keep a validated aggregate as one JSON payload while
// exposing the identifiers and lifecycle columns required for transactions,
// uniqueness constraints, recovery, and safe read projections.

func (s *Store) GetTaskSession(ctx context.Context, id domain.TaskSessionID) (domain.TaskSession, error) {
	var value domain.TaskSession
	return value, s.loadPayload(ctx, "task_sessions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveTaskSession(ctx context.Context, value domain.TaskSession) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO task_sessions (id, workspace_key, state, payload) VALUES (?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        workspace_key = excluded.workspace_key,
	        state = excluded.state,
	        payload = excluded.payload`,
		value.ID.String(),
		value.WorkspaceKey,
		string(value.State),
		payload,
	)
}

func (s *Store) GetTaskPacket(ctx context.Context, id domain.TaskPacketID) (domain.TaskPacket, error) {
	var value domain.TaskPacket
	return value, s.loadPayload(ctx, "task_packets", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveTaskPacket(ctx context.Context, value domain.TaskPacket) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(ctx, `INSERT INTO task_packets (id, payload) VALUES (?, ?)
        ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`, value.ID.String(), payload)
}

func (s *Store) GetContextManifest(ctx context.Context, id domain.ContextManifestID) (domain.ContextManifest, error) {
	var value domain.ContextManifest
	return value, s.loadPayload(ctx, "context_manifests", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveContextManifest(ctx context.Context, value domain.ContextManifest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(ctx, `INSERT INTO context_manifests (id, payload) VALUES (?, ?)
        ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`, value.ID.String(), payload)
}

func (s *Store) GetCapabilityGrant(ctx context.Context, id domain.CapabilityGrantID) (domain.CapabilityGrant, error) {
	var value domain.CapabilityGrant
	return value, s.loadPayload(ctx, "capability_grants", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveCapabilityGrant(ctx context.Context, value domain.CapabilityGrant) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO capability_grants (
	        id, workspace_key, context_manifest_id, approval_source, approval_policy_fingerprint, payload
	    ) VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        workspace_key = excluded.workspace_key,
	        context_manifest_id = excluded.context_manifest_id,
	        approval_source = excluded.approval_source,
	        approval_policy_fingerprint = excluded.approval_policy_fingerprint,
	        payload = excluded.payload`,
		value.ID.String(),
		value.WorkspaceKey,
		value.ContextManifestRef.String(),
		string(value.ApprovalSource),
		value.ApprovalPolicyFingerprint,
		payload,
	)
}

func (s *Store) GetDelegation(ctx context.Context, id domain.DelegationRequestID) (domain.DelegationRequest, error) {
	var value domain.DelegationRequest
	return value, s.loadPayload(
		ctx,
		"delegation_requests",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveDelegation(ctx context.Context, value domain.DelegationRequest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO delegation_requests (
	        id, task_session_id, source_thread_id, profile, task_packet_id,
	        context_manifest_id, capability_grant_id, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        profile = excluded.profile,
	        task_packet_id = excluded.task_packet_id,
	        context_manifest_id = excluded.context_manifest_id,
	        capability_grant_id = excluded.capability_grant_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		string(value.Profile),
		value.TaskPacketID.String(),
		value.ManifestID.String(),
		value.Grant.ID.String(),
		string(value.Status),
		payload,
	)
}

func (s *Store) GetAgentThread(ctx context.Context, id domain.AgentThreadID) (domain.AgentThread, error) {
	var value domain.AgentThread
	return value, s.loadPayload(ctx, "agent_threads", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentThread(ctx context.Context, value domain.AgentThread) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_threads (
        id, task_session_id, profile, task_packet_id, context_manifest_id, capability_grant_id, state, payload
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET task_session_id = excluded.task_session_id, profile = excluded.profile,
        task_packet_id = excluded.task_packet_id, context_manifest_id = excluded.context_manifest_id,
        capability_grant_id = excluded.capability_grant_id, state = excluded.state, payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		string(value.Profile),
		value.TaskPacketID.String(),
		value.ContextManifestID.String(),
		value.GrantID.String(),
		string(value.State),
		payload,
	)
}

func (s *Store) GetAgentRun(ctx context.Context, id domain.AgentRunID) (domain.AgentRun, error) {
	var value domain.AgentRun
	return value, s.loadPayload(ctx, "agent_runs", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentRun(ctx context.Context, value domain.AgentRun) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_runs (
	        id, agent_thread_id, work_item_id, sandbox_mode, approval_mode, outcome, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        agent_thread_id = excluded.agent_thread_id,
	        work_item_id = excluded.work_item_id,
	        sandbox_mode = excluded.sandbox_mode,
	        approval_mode = excluded.approval_mode,
	        outcome = excluded.outcome,
	        payload = excluded.payload`,
		value.ID.String(),
		value.AgentThreadID.String(),
		value.WorkItemID.String(),
		string(value.Execution.SandboxMode),
		string(value.Execution.ApprovalMode),
		string(value.Outcome),
		payload,
	)
}

func (s *Store) GetActiveByWorkspace(ctx context.Context, workspaceKey string) (domain.WorkspaceWriteLease, error) {
	row := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT payload FROM workspace_write_leases WHERE workspace_key = ? AND state = ?`,
		workspaceKey,
		string(domain.LeaseActive),
	)
	value, err := decodePayload[domain.WorkspaceWriteLease](
		row,
		func(value domain.WorkspaceWriteLease) error { return value.Validate() },
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceWriteLease{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkspaceWriteLease{}, fmt.Errorf("get active workspace lease: %w", err)
	}
	return value, nil
}

func (s *Store) SaveWorkspaceLease(ctx context.Context, value domain.WorkspaceWriteLease) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO workspace_write_leases (
        id, workspace_key, owner_thread_id, capability_grant_id, state, payload
    ) VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET workspace_key = excluded.workspace_key, owner_thread_id = excluded.owner_thread_id,
        capability_grant_id = excluded.capability_grant_id, state = excluded.state, payload = excluded.payload`,
		value.ID.String(),
		value.WorkspaceKey,
		value.OwnerThreadID.String(),
		value.GrantID.String(),
		string(value.State),
		payload,
	)
	if err != nil && isConstraintError(err, "workspace_write_leases.workspace_key") {
		return fmt.Errorf("%w: %s", domain.ErrLeaseConflict, value.WorkspaceKey)
	}
	return err
}

func (s *Store) ReleaseWorkspaceLease(ctx context.Context, value domain.WorkspaceWriteLease) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.State != domain.LeaseReleased {
		return errors.New("workspace lease release requires a released lease")
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	result, err := executorFromContext(ctx, s.db).ExecContext(
		ctx,
		`UPDATE workspace_write_leases SET state = ?, payload = ? WHERE id = ? AND state = ?`,
		string(domain.LeaseReleased),
		payload,
		value.ID.String(),
		string(domain.LeaseActive),
	)
	if err != nil {
		return fmt.Errorf("release workspace lease: %w", err)
	}
	return exactlyOne(result, "release workspace lease")
}

func (s *Store) GetAgentResult(ctx context.Context, id domain.AgentResultID) (domain.AgentResult, error) {
	var value domain.AgentResult
	return value, s.loadPayload(ctx, "agent_results", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentResult(ctx context.Context, value domain.AgentResult) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_results (id, task_session_id, source_thread_id, status, payload) VALUES (?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		string(value.Status),
		payload,
	)
}

func (s *Store) GetBriefing(ctx context.Context, id domain.BriefingID) (domain.Briefing, error) {
	var value domain.Briefing
	return value, s.loadPayload(ctx, "briefings", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveBriefing(ctx context.Context, value domain.Briefing) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO briefings (
	        id, task_session_id, source_thread_id, target_thread_id, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        target_thread_id = excluded.target_thread_id,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		value.TargetThreadID.String(),
		string(value.Status),
		payload,
	)
}

func (s *Store) GetDelivery(ctx context.Context, id domain.DeliveryID) (domain.BriefingDelivery, error) {
	var value domain.BriefingDelivery
	return value, s.loadPayload(
		ctx,
		"briefing_deliveries",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) GetDeliveryByInjectionKey(ctx context.Context, injectionKey string) (domain.BriefingDelivery, error) {
	row := executorFromContext(
		ctx,
		s.db,
	).QueryRowContext(ctx, `SELECT payload FROM briefing_deliveries WHERE injection_key = ?`, injectionKey)
	value, err := decodePayload[domain.BriefingDelivery](
		row,
		func(value domain.BriefingDelivery) error { return value.Validate() },
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.BriefingDelivery{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.BriefingDelivery{}, fmt.Errorf("get delivery by injection key: %w", err)
	}
	return value, nil
}

func (s *Store) SaveDelivery(ctx context.Context, value domain.BriefingDelivery) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO briefing_deliveries (
	        id, briefing_id, target_thread_id, injection_key, status, payload
	    ) VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        briefing_id = excluded.briefing_id,
	        target_thread_id = excluded.target_thread_id,
	        injection_key = excluded.injection_key,
	        status = excluded.status,
	        payload = excluded.payload`,
		value.ID.String(),
		value.BriefingID.String(),
		value.TargetThreadID.String(),
		value.InjectionKey,
		string(value.Status),
		payload,
	)
	if err != nil && isConstraintError(err, "briefing_deliveries.injection_key") {
		return fmt.Errorf("%w: %s", domain.ErrAlreadyDelivered, value.InjectionKey)
	}
	return err
}

func (s *Store) GetNote(ctx context.Context, id domain.NoteID) (domain.Note, error) {
	var value domain.Note
	return value, s.loadPayload(ctx, "notes", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveNote(ctx context.Context, value domain.Note) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO notes (id, task_session_id, source_thread_id, payload) VALUES (?, ?, ?, ?)
	    ON CONFLICT(id) DO UPDATE SET
	        task_session_id = excluded.task_session_id,
	        source_thread_id = excluded.source_thread_id,
	        payload = excluded.payload`,
		value.ID.String(),
		value.TaskSessionID.String(),
		value.SourceThreadID.String(),
		payload,
	)
}

func (s *Store) AppendEvent(ctx context.Context, value domain.DomainEvent) error {
	if value.ID == "" || value.Type == "" || value.OccurredAt.IsZero() {
		return errors.New("domain event identity, type and occurrence time are required")
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO orchestration_events (
	        id, task_session_id, agent_thread_id, agent_run_id, work_item_id,
	        delegation_request_id, delivery_id, occurred_at, event_type, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID.String(),
		value.TaskSession.String(),
		value.AgentThread.String(),
		value.AgentRun.String(),
		value.WorkItem.String(),
		value.Delegation.String(),
		value.Delivery.String(),
		nullableTimeValue(value.OccurredAt),
		string(value.Type),
		payload,
	)
}

func (s *Store) loadPayload(ctx context.Context, table, id string, target any, validate func() error) error {
	row := executorFromContext(ctx, s.db).QueryRowContext(ctx, `SELECT payload FROM `+table+` WHERE id = ?`, id)
	var payload []byte
	if err := row.Scan(&payload); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode stored %s: %w", table, err)
	}
	if err := validate(); err != nil {
		return fmt.Errorf("validate stored %s: %w", table, err)
	}
	return nil
}

func decodePayload[T any](row *sql.Row, validate func(T) error) (T, error) {
	var zero T
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return zero, err
	}
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return zero, err
	}
	if err := validate(value); err != nil {
		return zero, err
	}
	return value, nil
}

func encodePayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode product payload: %w", err)
	}
	var checked any
	if err := json.Unmarshal(payload, &checked); err != nil {
		return nil, fmt.Errorf("inspect product payload: %w", err)
	}
	if hasSensitiveKey(checked) {
		return nil, errors.New("refusing to persist a payload with a sensitive field")
	}
	return payload, nil
}

func hasSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "apikey",
				"api_key",
				"authorization",
				"secret",
				"token",
				"providerpayload",
				"provider_payload",
				"hiddenprompt",
				"hidden_prompt":
				return true
			}
			if hasSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}

func (s *Store) savePayload(ctx context.Context, query string, args ...any) error {
	if _, ok := contextTx(ctx); !ok {
		return errors.New("product mutation requires sqlite transaction")
	}
	if _, err := executorFromContext(ctx, s.db).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("persist product payload: %w", err)
	}
	return nil
}

func exactlyOne(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check %s: %w", operation, err)
	}
	if affected != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func isConstraintError(err error, column string) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed: "+column)
}

// RecoveryThreadRef identifies a session file that must be checked before
// product state is marked interrupted during process-start reconciliation.
type RecoveryThreadRef struct {
	TaskSessionID domain.TaskSessionID
	AgentThreadID domain.AgentThreadID
}

func (s *Store) ListRecoveryThreads(ctx context.Context) ([]RecoveryThreadRef, error) {
	rows, err := executorFromContext(
		ctx,
		s.db,
	).QueryContext(ctx, `SELECT task_session_id, id FROM agent_threads ORDER BY task_session_id, id`)
	if err != nil {
		return nil, fmt.Errorf("list recovery threads: %w", err)
	}
	defer rows.Close()
	refs := make([]RecoveryThreadRef, 0)
	for rows.Next() {
		var sessionID, threadID string
		if err := rows.Scan(&sessionID, &threadID); err != nil {
			return nil, fmt.Errorf("scan recovery thread: %w", err)
		}
		refs = append(
			refs,
			RecoveryThreadRef{
				TaskSessionID: domain.TaskSessionID(sessionID),
				AgentThreadID: domain.AgentThreadID(threadID),
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery threads: %w", err)
	}
	return refs, nil
}

func (s *Store) ListUnsettledRunIDs(ctx context.Context) ([]domain.AgentRunID, error) {
	rows, err := executorFromContext(
		ctx,
		s.db,
	).QueryContext(ctx, `SELECT id FROM agent_runs WHERE outcome = '' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list unsettled runs: %w", err)
	}
	defer rows.Close()
	ids := make([]domain.AgentRunID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan unsettled run: %w", err)
		}
		ids = append(ids, domain.AgentRunID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unsettled runs: %w", err)
	}
	return ids, nil
}

func (s *Store) ListActiveLeaseIDs(ctx context.Context) ([]domain.WorkspaceLeaseID, error) {
	rows, err := executorFromContext(
		ctx,
		s.db,
	).QueryContext(ctx, `SELECT id FROM workspace_write_leases WHERE state = ? ORDER BY id`, string(domain.LeaseActive))
	if err != nil {
		return nil, fmt.Errorf("list active workspace leases: %w", err)
	}
	defer rows.Close()
	ids := make([]domain.WorkspaceLeaseID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan active workspace lease: %w", err)
		}
		ids = append(ids, domain.WorkspaceLeaseID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active workspace leases: %w", err)
	}
	return ids, nil
}

func (s *Store) GetWorkspaceLease(ctx context.Context, id domain.WorkspaceLeaseID) (domain.WorkspaceWriteLease, error) {
	row := executorFromContext(
		ctx,
		s.db,
	).QueryRowContext(ctx, `SELECT payload FROM workspace_write_leases WHERE id = ?`, id.String())
	value, err := decodePayload[domain.WorkspaceWriteLease](
		row,
		func(value domain.WorkspaceWriteLease) error { return value.Validate() },
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceWriteLease{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkspaceWriteLease{}, fmt.Errorf("get workspace lease: %w", err)
	}
	return value, nil
}

func (s *Store) ListInFlightDeliveryIDs(ctx context.Context) ([]domain.DeliveryID, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT id FROM briefing_deliveries WHERE status IN (?, ?) ORDER BY id`,
		string(domain.DeliveryPending),
		string(domain.DeliveryDelivering),
	)
	if err != nil {
		return nil, fmt.Errorf("list in-flight deliveries: %w", err)
	}
	defer rows.Close()
	ids := make([]domain.DeliveryID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan in-flight delivery: %w", err)
		}
		ids = append(ids, domain.DeliveryID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate in-flight deliveries: %w", err)
	}
	return ids, nil
}

// Repositories contains typed views over one SQLite connection. It preserves
// the core's narrow aggregate ports without creating competing transaction or
// schema owners for the same product database.
type Repositories struct {
	TaskSessions     TaskSessionRepository
	TaskPackets      TaskPacketRepository
	ContextManifests ContextManifestRepository
	CapabilityGrants CapabilityGrantRepository
	Delegations      DelegationRepository
	AgentThreads     AgentThreadRepository
	AgentRuns        AgentRunRepository
	WorkspaceLeases  WorkspaceLeaseRepository
	AgentResults     AgentResultRepository
	Briefings        BriefingRepository
	Deliveries       DeliveryRepository
	Notes            NoteRepository
	Events           EventRepository
}

// Repositories returns all aggregate views backed by this Store.
func (s *Store) Repositories() Repositories {
	return Repositories{
		TaskSessions:     TaskSessionRepository{s},
		TaskPackets:      TaskPacketRepository{s},
		ContextManifests: ContextManifestRepository{s},
		CapabilityGrants: CapabilityGrantRepository{s},
		Delegations:      DelegationRepository{s},
		AgentThreads:     AgentThreadRepository{s},
		AgentRuns:        AgentRunRepository{s},
		WorkspaceLeases:  WorkspaceLeaseRepository{s},
		AgentResults:     AgentResultRepository{s},
		Briefings:        BriefingRepository{s},
		Deliveries:       DeliveryRepository{s},
		Notes:            NoteRepository{s},
		Events:           EventRepository{s},
	}
}

type TaskSessionRepository struct{ store *Store }

func (r TaskSessionRepository) Get(ctx context.Context, id domain.TaskSessionID) (domain.TaskSession, error) {
	return r.store.GetTaskSession(ctx, id)
}
func (r TaskSessionRepository) Save(ctx context.Context, value domain.TaskSession) error {
	return r.store.SaveTaskSession(ctx, value)
}

type TaskPacketRepository struct{ store *Store }

func (r TaskPacketRepository) Get(ctx context.Context, id domain.TaskPacketID) (domain.TaskPacket, error) {
	return r.store.GetTaskPacket(ctx, id)
}
func (r TaskPacketRepository) Save(ctx context.Context, value domain.TaskPacket) error {
	return r.store.SaveTaskPacket(ctx, value)
}

type ContextManifestRepository struct{ store *Store }

func (r ContextManifestRepository) Get(
	ctx context.Context,
	id domain.ContextManifestID,
) (domain.ContextManifest, error) {
	return r.store.GetContextManifest(ctx, id)
}
func (r ContextManifestRepository) Save(ctx context.Context, value domain.ContextManifest) error {
	return r.store.SaveContextManifest(ctx, value)
}

type CapabilityGrantRepository struct{ store *Store }

func (r CapabilityGrantRepository) Get(
	ctx context.Context,
	id domain.CapabilityGrantID,
) (domain.CapabilityGrant, error) {
	return r.store.GetCapabilityGrant(ctx, id)
}
func (r CapabilityGrantRepository) Save(ctx context.Context, value domain.CapabilityGrant) error {
	return r.store.SaveCapabilityGrant(ctx, value)
}

type DelegationRepository struct{ store *Store }

func (r DelegationRepository) Get(
	ctx context.Context,
	id domain.DelegationRequestID,
) (domain.DelegationRequest, error) {
	return r.store.GetDelegation(ctx, id)
}
func (r DelegationRepository) Save(ctx context.Context, value domain.DelegationRequest) error {
	return r.store.SaveDelegation(ctx, value)
}

type AgentThreadRepository struct{ store *Store }

func (r AgentThreadRepository) Get(ctx context.Context, id domain.AgentThreadID) (domain.AgentThread, error) {
	return r.store.GetAgentThread(ctx, id)
}
func (r AgentThreadRepository) Save(ctx context.Context, value domain.AgentThread) error {
	return r.store.SaveAgentThread(ctx, value)
}

type AgentRunRepository struct{ store *Store }

func (r AgentRunRepository) Get(ctx context.Context, id domain.AgentRunID) (domain.AgentRun, error) {
	return r.store.GetAgentRun(ctx, id)
}
func (r AgentRunRepository) Save(ctx context.Context, value domain.AgentRun) error {
	return r.store.SaveAgentRun(ctx, value)
}

type WorkspaceLeaseRepository struct{ store *Store }

func (r WorkspaceLeaseRepository) GetActiveByWorkspace(
	ctx context.Context,
	key string,
) (domain.WorkspaceWriteLease, error) {
	return r.store.GetActiveByWorkspace(ctx, key)
}
func (r WorkspaceLeaseRepository) Save(ctx context.Context, value domain.WorkspaceWriteLease) error {
	return r.store.SaveWorkspaceLease(ctx, value)
}
func (r WorkspaceLeaseRepository) Release(ctx context.Context, value domain.WorkspaceWriteLease) error {
	return r.store.ReleaseWorkspaceLease(ctx, value)
}

type AgentResultRepository struct{ store *Store }

func (r AgentResultRepository) Get(ctx context.Context, id domain.AgentResultID) (domain.AgentResult, error) {
	return r.store.GetAgentResult(ctx, id)
}
func (r AgentResultRepository) Save(ctx context.Context, value domain.AgentResult) error {
	return r.store.SaveAgentResult(ctx, value)
}

type BriefingRepository struct{ store *Store }

func (r BriefingRepository) Get(ctx context.Context, id domain.BriefingID) (domain.Briefing, error) {
	return r.store.GetBriefing(ctx, id)
}
func (r BriefingRepository) Save(ctx context.Context, value domain.Briefing) error {
	return r.store.SaveBriefing(ctx, value)
}

type DeliveryRepository struct{ store *Store }

func (r DeliveryRepository) Get(ctx context.Context, id domain.DeliveryID) (domain.BriefingDelivery, error) {
	return r.store.GetDelivery(ctx, id)
}
func (r DeliveryRepository) GetByInjectionKey(ctx context.Context, key string) (domain.BriefingDelivery, error) {
	return r.store.GetDeliveryByInjectionKey(ctx, key)
}
func (r DeliveryRepository) Save(ctx context.Context, value domain.BriefingDelivery) error {
	return r.store.SaveDelivery(ctx, value)
}

type NoteRepository struct{ store *Store }

func (r NoteRepository) Get(ctx context.Context, id domain.NoteID) (domain.Note, error) {
	return r.store.GetNote(ctx, id)
}
func (r NoteRepository) Save(ctx context.Context, value domain.Note) error {
	return r.store.SaveNote(ctx, value)
}

type EventRepository struct{ store *Store }

func (r EventRepository) Append(ctx context.Context, value domain.DomainEvent) error {
	return r.store.AppendEvent(ctx, value)
}

var (
	_ persistence.TaskSessionRepository     = TaskSessionRepository{}
	_ persistence.TaskPacketRepository      = TaskPacketRepository{}
	_ persistence.ContextManifestRepository = ContextManifestRepository{}
	_ persistence.CapabilityGrantRepository = CapabilityGrantRepository{}
	_ persistence.DelegationRepository      = DelegationRepository{}
	_ persistence.AgentThreadRepository     = AgentThreadRepository{}
	_ persistence.AgentRunRepository        = AgentRunRepository{}
	_ persistence.WorkspaceLeaseRepository  = WorkspaceLeaseRepository{}
	_ persistence.AgentResultRepository     = AgentResultRepository{}
	_ persistence.BriefingRepository        = BriefingRepository{}
	_ persistence.DeliveryRepository        = DeliveryRepository{}
	_ persistence.NoteRepository            = NoteRepository{}
	_ persistence.EventRepository           = EventRepository{}
)
