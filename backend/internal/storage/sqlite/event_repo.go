package sqlite

import (
	"context"
	"errors"

	"praxis/internal/core/domain"
)

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

func (r EventRepository) Append(ctx context.Context, value domain.DomainEvent) error {
	return r.store.AppendEvent(ctx, value)
}
