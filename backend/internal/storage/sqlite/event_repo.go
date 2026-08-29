package sqlite

import (
	"context"
	"errors"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
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
