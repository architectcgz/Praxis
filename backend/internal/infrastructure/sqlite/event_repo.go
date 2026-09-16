package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	"praxis/internal/persistence"
)

// AppendEvent inserts one relational audit row inside the owning business
// transaction. Events carry explicit fields only and never create documents.
func (s *Store) AppendEvent(ctx context.Context, value domainfoundation.DomainEvent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	err := s.execMutation(ctx, `INSERT INTO orchestration_events (
		id, event_type, occurred_at, project_id, workspace_id, session_id, agent_id,
		target_agent_id, execution_id, work_item_id, delegation_id, delivery_id,
		artifact_kind, artifact_id, approval_source, policy_revision, context_revision,
		context_kind, execution_outcome, failure_code
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID.String(), string(value.Type), value.OccurredAt.UTC().Format(time.RFC3339Nano),
		nullableID(value.ProjectID), nullableID(value.WorkspaceID), nullableID(value.SessionID),
		nullableID(value.AgentID), nullableID(value.TargetAgentID), nullableID(value.AgentExecutionID),
		nullableID(value.WorkItemID), nullableID(value.DelegationID), nullableID(value.DeliveryID),
		nullableText(value.ArtifactKind), nullableText(value.ArtifactID), nullableText(value.ApprovalSource),
		nullableUint(value.PolicyRevision), nullableUint(value.ContextRevision), nullableText(value.ContextKind),
		nullableText(value.ExecutionOutcome), nullableText(value.FailureCode),
	)
	if err != nil && isConstraintError(err, "orchestration_events.id") {
		return fmt.Errorf("%w: %s", domainfoundation.ErrRequestConflict, value.ID)
	}
	return err
}

func (s *Store) ListSessionEvents(ctx context.Context, id domainfoundation.SessionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return s.listEvents(ctx, "session_id", id.String(), after, limit)
}

func (s *Store) ListAgentEvents(ctx context.Context, id domainfoundation.AgentID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return s.listEvents(ctx, "agent_id", id.String(), after, limit)
}

func (s *Store) ListExecutionEvents(ctx context.Context, id domainfoundation.AgentExecutionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return s.listEvents(ctx, "execution_id", id.String(), after, limit)
}

func (s *Store) listEvents(ctx context.Context, column, id string, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	cutoff := ""
	if !after.IsZero() {
		cutoff = after.UTC().Format(time.RFC3339Nano)
	}
	rows, err := s.Executor(ctx).QueryContext(ctx,
		`SELECT `+eventColumns+` FROM orchestration_events WHERE `+column+` = ? AND occurred_at > ? ORDER BY occurred_at, id LIMIT ?`,
		id, cutoff, targetLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list orchestration events: %w", err)
	}
	defer rows.Close()
	events := make([]domainfoundation.DomainEvent, 0)
	for rows.Next() {
		value, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orchestration events: %w", err)
	}
	return events, nil
}

const eventColumns = `id, event_type, occurred_at, project_id, workspace_id, session_id, agent_id,
	target_agent_id, execution_id, work_item_id, delegation_id, delivery_id,
	artifact_kind, artifact_id, approval_source, policy_revision, context_revision,
	context_kind, execution_outcome, failure_code`

func scanEvent(rows *sql.Rows) (domainfoundation.DomainEvent, error) {
	var (
		id, eventType, occurredAt            string
		projectID, workspaceID, sessionID    sql.NullString
		agentID, targetAgentID, executionID  sql.NullString
		workItemID, delegationID, deliveryID sql.NullString
		artifactKind, artifactID, approval   sql.NullString
		contextKind, outcome, failureCode    sql.NullString
		policyRevision, contextRevision      sql.NullInt64
	)
	if err := rows.Scan(
		&id, &eventType, &occurredAt, &projectID, &workspaceID, &sessionID, &agentID,
		&targetAgentID, &executionID, &workItemID, &delegationID, &deliveryID,
		&artifactKind, &artifactID, &approval, &policyRevision, &contextRevision,
		&contextKind, &outcome, &failureCode,
	); err != nil {
		return domainfoundation.DomainEvent{}, fmt.Errorf("scan orchestration event: %w", err)
	}
	at, err := parseTimestamp(occurredAt, "orchestration event.occurredAt")
	if err != nil {
		return domainfoundation.DomainEvent{}, err
	}
	value := domainfoundation.DomainEvent{
		ID:               domainfoundation.EventID(id),
		Type:             domainfoundation.DomainEventType(eventType),
		OccurredAt:       at,
		ProjectID:        domainfoundation.ProjectID(eventText(projectID)),
		WorkspaceID:      domainfoundation.WorkspaceID(eventText(workspaceID)),
		SessionID:        domainfoundation.SessionID(eventText(sessionID)),
		AgentID:          domainfoundation.AgentID(eventText(agentID)),
		TargetAgentID:    domainfoundation.AgentID(eventText(targetAgentID)),
		AgentExecutionID: domainfoundation.AgentExecutionID(eventText(executionID)),
		WorkItemID:       domainfoundation.WorkItemID(eventText(workItemID)),
		DelegationID:     domainfoundation.DelegationRequestID(eventText(delegationID)),
		DeliveryID:       domainfoundation.DeliveryID(eventText(deliveryID)),
		ArtifactKind:     eventText(artifactKind),
		ArtifactID:       eventText(artifactID),
		ApprovalSource:   eventText(approval),
		PolicyRevision:   eventUint(policyRevision),
		ContextRevision:  eventUint(contextRevision),
		ContextKind:      eventText(contextKind),
		ExecutionOutcome: eventText(outcome),
		FailureCode:      eventText(failureCode),
	}
	if err := value.Validate(); err != nil {
		return domainfoundation.DomainEvent{}, fmt.Errorf("validate stored orchestration event: %w", err)
	}
	return value, nil
}

func eventText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func eventUint(value sql.NullInt64) uint64 {
	if value.Valid && value.Int64 > 0 {
		return uint64(value.Int64)
	}
	return 0
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableUint(value uint64) any {
	if value == 0 {
		return nil
	}
	return value
}

// EventRepository exposes relational audit events without the Document Store.
type EventRepository struct{ store *Store }

func (r EventRepository) Append(ctx context.Context, value domainfoundation.DomainEvent) error {
	return r.store.AppendEvent(ctx, value)
}

func (r EventRepository) ListBySession(ctx context.Context, id domainfoundation.SessionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return r.store.ListSessionEvents(ctx, id, after, limit)
}

func (r EventRepository) ListByAgent(ctx context.Context, id domainfoundation.AgentID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return r.store.ListAgentEvents(ctx, id, after, limit)
}

func (r EventRepository) ListByExecution(ctx context.Context, id domainfoundation.AgentExecutionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return r.store.ListExecutionEvents(ctx, id, after, limit)
}

var _ persistence.EventRepository = EventRepository{}
var _ persistence.EventQueryRepository = EventRepository{}
