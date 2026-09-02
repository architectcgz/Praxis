package sqlite

import (
	"context"
	"errors"
	"time"

	domainfoundation "praxis/internal/core/domain/foundation"

	"praxis/internal/core/persistence"
)

func (s *Store) AppendEvent(ctx context.Context, value domainfoundation.DomainEvent) error {
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
	        id, session_id, agent_id, execution_id, work_item_id,
	        delegation_id, delivery_id, occurred_at, event_type, payload
	    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID.String(),
		value.SessionID.String(),
		value.AgentID.String(),
		value.AgentExecutionID.String(),
		value.WorkItemID.String(),
		value.DelegationID.String(),
		value.DeliveryID.String(),
		nullableTimeValue(value.OccurredAt),
		string(value.Type),
		payload,
	)
}

type EventRepository struct{ store *Store }

func (r EventRepository) Append(ctx context.Context, value domainfoundation.DomainEvent) error {
	return r.store.AppendEvent(ctx, value)
}

func (r EventRepository) ListBySession(ctx context.Context, id domainfoundation.SessionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return r.list(ctx, "session_id", id.String(), after, limit)
}

func (r EventRepository) ListByAgent(ctx context.Context, id domainfoundation.AgentID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return r.list(ctx, "agent_id", id.String(), after, limit)
}

func (r EventRepository) ListByExecution(ctx context.Context, id domainfoundation.AgentExecutionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	return r.list(ctx, "execution_id", id.String(), after, limit)
}

func (r EventRepository) list(ctx context.Context, column, id string, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	cutoff := ""
	if !after.IsZero() {
		cutoff = after.UTC().Format(time.RFC3339Nano)
	}
	return listTargetPayloads[domainfoundation.DomainEvent](ctx, r.store,
		`SELECT payload FROM orchestration_events WHERE `+column+` = ? AND occurred_at > ? ORDER BY occurred_at, id LIMIT ?`,
		[]any{id, cutoff, targetLimit(limit)}, "orchestration events", func(value domainfoundation.DomainEvent) error {
			if value.ID == "" || value.Type == "" || value.OccurredAt.IsZero() {
				return errors.New("invalid stored orchestration event")
			}
			return nil
		})
}

var _ persistence.EventQueryRepository = EventRepository{}
