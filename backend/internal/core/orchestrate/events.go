package orchestrate

import (
	"context"
	domainfoundation "praxis/internal/core/domain/foundation"
	"time"
)

func (o *AgentOrchestrator) newEvent(eventType domainfoundation.DomainEventType, at time.Time) domainfoundation.DomainEvent {
	return domainfoundation.DomainEvent{ID: domainfoundation.EventID(o.newID("event")), Type: eventType, OccurredAt: at.UTC()}
}

func (o *AgentOrchestrator) appendEvent(ctx context.Context, event domainfoundation.DomainEvent) error {
	return o.events.Append(ctx, event)
}
