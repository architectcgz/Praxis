package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	agentrun "praxis/internal/core/agent/run"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"time"
)

type storedEvent struct {
	Type      string            `json:"type"`
	Key       string            `json:"key"`
	AgentID   identity.AgentID  `json:"agent_id"`
	BranchID  identity.BranchID `json:"branch_id,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Payload   json.RawMessage   `json:"payload"`
}

func encodeEvent(event sessionmodel.Event) ([]byte, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(storedEvent{
		Type:      event.Type,
		Key:       event.Key,
		AgentID:   event.AgentID,
		BranchID:  event.BranchID,
		CreatedAt: event.CreatedAt,
		Payload:   payload,
	})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func decodeEvent(line []byte) (sessionmodel.Event, error) {
	var stored storedEvent
	if err := decodeEventJSON(line, &stored); err != nil {
		return sessionmodel.Event{}, fmt.Errorf("expected a single session event: %w", err)
	}
	event := sessionmodel.Event{
		Type:      stored.Type,
		Key:       stored.Key,
		AgentID:   stored.AgentID,
		BranchID:  stored.BranchID,
		CreatedAt: stored.CreatedAt,
	}
	var err error
	switch stored.Type {
	case sessionmodel.EventRunSaved:
		event.Payload, err = decodeEventPayload[agentrun.RunRecord](stored.Payload)
	case sessionmodel.EventRunEnded:
		event.Payload, err = decodeEventPayload[sessionmodel.RunEnded](stored.Payload)
	case sessionmodel.EventEntryAppended:
		event.Payload, err = decodeEventPayload[sessionentry.ContextEntry](stored.Payload)
	case sessionmodel.EventModelRequestStarted, sessionmodel.EventModelRequestFinished:
		event.Payload, err = decodeEventPayload[sessionmodel.ModelRequestSaved](stored.Payload)
	default:
		return sessionmodel.Event{}, fmt.Errorf("unknown session event type %q", stored.Type)
	}
	return event, err
}

func decodeEventPayload[T any](data []byte) (any, error) {
	var value T
	if err := decodeEventJSON(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeEventJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON data")
	}
	return nil
}
