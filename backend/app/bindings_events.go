package app

import (
	"context"
	"time"

	"praxis/internal/contracts"
	domainfoundation "praxis/internal/domain/foundation"
)

// EventBindings exposes durable orchestration events as a query-only surface.
// Callers use the occurred-at cursor to converge projections after a wakeup.
type EventBindings struct {
	runtime *bindingRuntime
	queries serviceRef[EventQueries]
}

func (b *EventBindings) ListSessionEvents(sessionID string, after time.Time, limit int) ([]contracts.EventSnapshot, error) {
	return b.list(func(q EventQueries, ctx context.Context) ([]domainfoundation.DomainEvent, error) {
		return q.ListSessionEvents(ctx, domainfoundation.SessionID(sessionID), after, limit)
	})
}

func (b *EventBindings) ListAgentEvents(agentID string, after time.Time, limit int) ([]contracts.EventSnapshot, error) {
	return b.list(func(q EventQueries, ctx context.Context) ([]domainfoundation.DomainEvent, error) {
		return q.ListAgentEvents(ctx, domainfoundation.AgentID(agentID), after, limit)
	})
}

func (b *EventBindings) ListExecutionEvents(executionID string, after time.Time, limit int) ([]contracts.EventSnapshot, error) {
	return b.list(func(q EventQueries, ctx context.Context) ([]domainfoundation.DomainEvent, error) {
		return q.ListExecutionEvents(ctx, domainfoundation.AgentExecutionID(executionID), after, limit)
	})
}

func (b *EventBindings) list(query func(EventQueries, context.Context) ([]domainfoundation.DomainEvent, error)) ([]contracts.EventSnapshot, error) {
	ctx, err := b.runtime.context()
	if err != nil {
		return nil, err
	}
	queries := b.queries.get()
	if queries == nil {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	events, err := query(queries, ctx)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.EventSnapshot, 0, len(events))
	for _, event := range events {
		result = append(result, contracts.EventSnapshot{
			ID: event.ID.String(), Type: string(event.Type), OccurredAt: event.OccurredAt,
			SessionID: event.SessionID.String(), AgentID: event.AgentID.String(),
			ExecutionID: event.AgentExecutionID.String(), Payload: cloneEventPayload(event.Payload),
		})
	}
	return result, nil
}

func cloneEventPayload(payload map[string]string) map[string]string {
	if payload == nil {
		return nil
	}
	copy := make(map[string]string, len(payload))
	for key, value := range payload {
		copy[key] = value
	}
	return copy
}
