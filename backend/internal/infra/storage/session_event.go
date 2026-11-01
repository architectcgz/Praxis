package storage

import (
	"encoding/json"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"time"
)

type storedEvent struct {
	Type       string          `json:"type"`
	Collection string          `json:"collection"`
	Key        string          `json:"key"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload"`
}

func encodeEvent(event sessionmodel.Event) ([]byte, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(storedEvent{
		Type:       event.Type,
		Collection: event.Collection,
		Key:        event.Key,
		CreatedAt:  event.CreatedAt,
		Payload:    payload,
	})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func decodeEvent(line []byte) (sessionmodel.Event, error) {
	var stored storedEvent
	if err := decodeJSON(line, &stored); err != nil {
		return sessionmodel.Event{}, fmt.Errorf("expected a single storage event: %w", err)
	}
	event := sessionmodel.Event{
		Type:       stored.Type,
		Collection: stored.Collection,
		Key:        stored.Key,
		CreatedAt:  stored.CreatedAt,
	}
	var err error
	switch stored.Type {
	case sessionmodel.EventBranchCreated:
		event.Payload, err = decodeEventPayload[sessionmodel.Branch](stored.Payload)
	case sessionmodel.EventExecutionSaved:
		event.Payload, err = decodeEventPayload[agentexecution.ExecutionRecord](stored.Payload)
	case sessionmodel.EventEntryAppended:
		event.Payload, err = decodeEventPayload[sessionentry.ContextEntry](stored.Payload)
	case sessionmodel.EventUsageSaved:
		event.Payload, err = decodeEventPayload[providerapi.UsageRecord](stored.Payload)
	case sessionmodel.EventTimingStarted, sessionmodel.EventTimingFinished:
		event.Payload, err = decodeEventPayload[timing.Record](stored.Payload)
	case "wait.saved":
		// 审批等待不参与重启恢复。
	default:
		return sessionmodel.Event{}, fmt.Errorf("unknown stored event type %q", stored.Type)
	}
	return event, err
}

func decodeEventPayload[T any](data []byte) (any, error) {
	var value T
	if err := decodeJSON(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}
