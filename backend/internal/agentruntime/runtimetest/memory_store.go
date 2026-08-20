package runtimetest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"praxis/internal/agentruntime"
)

// MemorySessionStore is a thread-safe semantic session store for local runtime tests.
// It intentionally stores runtime events, not a parallel JSONL persistence schema.
type MemorySessionStore struct {
	mu          sync.Mutex
	manifest    *agentruntime.SessionManifest
	events      []agentruntime.SessionEvent
	AppendError error
	FlushError  error
}

// Initialize implements agentruntime.AgentSessionStore.
func (s *MemorySessionStore) Initialize(
	_ context.Context,
	manifest agentruntime.SessionManifest,
	_ time.Time,
) (agentruntime.SessionAppendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AppendError != nil {
		return agentruntime.SessionAppendResult{}, s.AppendError
	}
	if s.manifest != nil || len(s.events) != 0 {
		return agentruntime.SessionAppendResult{}, fmt.Errorf("session is already initialized")
	}
	copy := manifest
	s.manifest = &copy
	return agentruntime.SessionAppendResult{EntryID: "entry-1", Sequence: 1}, nil
}

// Append implements agentruntime.AgentSessionStore.
func (s *MemorySessionStore) Append(
	_ context.Context,
	event agentruntime.SessionEvent,
) (agentruntime.SessionAppendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AppendError != nil {
		return agentruntime.SessionAppendResult{}, s.AppendError
	}
	if s.manifest == nil {
		return agentruntime.SessionAppendResult{}, fmt.Errorf("session must be initialized before appending events")
	}
	s.events = append(s.events, cloneSessionEvent(event))
	sequence := uint64(len(s.events) + 1)
	return agentruntime.SessionAppendResult{EntryID: fmt.Sprintf("entry-%d", sequence), Sequence: sequence}, nil
}

// Flush implements agentruntime.SessionFlusher.
func (s *MemorySessionStore) Flush(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.FlushError
}

// Events returns defensive copies of appended runtime events.
func (s *MemorySessionStore) Events() []agentruntime.SessionEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]agentruntime.SessionEvent, len(s.events))
	for index, event := range s.events {
		result[index] = cloneSessionEvent(event)
	}
	return result
}

// ReadContext implements agentruntime.AgentSessionStore.
func (s *MemorySessionStore) ReadContext(_ context.Context, _ string) (agentruntime.SessionContext, error) {
	s.mu.Lock()
	hasManifest := s.manifest != nil
	events := make([]agentruntime.SessionEvent, len(s.events))
	for index, event := range s.events {
		events[index] = cloneSessionEvent(event)
	}
	s.mu.Unlock()
	projection := agentruntime.SessionContext{HasManifest: hasManifest, CanContinue: hasManifest}
	for _, event := range events {
		switch payload := event.Payload.(type) {
		case agentruntime.MessageEvent:
			projection.Messages = append(projection.Messages, cloneTurnMessage(payload.Message))
		}
	}
	return projection, nil
}

func cloneSessionEvent(event agentruntime.SessionEvent) agentruntime.SessionEvent {
	copy := event
	switch payload := event.Payload.(type) {
	case agentruntime.RunStartedEvent:
		payload.Inputs = payload.Inputs.Snapshot()
		copy.Payload = payload
	case agentruntime.MessageEvent:
		copy.Payload = agentruntime.MessageEvent{Message: cloneTurnMessage(payload.Message)}
	case agentruntime.QueueEnqueuedEvent:
		copy.Payload = agentruntime.QueueEnqueuedEvent{
			Queue:   payload.Queue,
			ItemID:  payload.ItemID,
			Content: cloneTurnContent(payload.Content),
		}
	}
	return copy
}

func cloneTurnMessage(message agentruntime.TurnMessage) agentruntime.TurnMessage {
	return agentruntime.TurnMessage{Role: message.Role, Content: cloneTurnContent(message.Content)}
}

func cloneTurnContent(content []agentruntime.TurnContentBlock) []agentruntime.TurnContentBlock {
	result := make([]agentruntime.TurnContentBlock, len(content))
	for index, block := range content {
		result[index] = block
		result[index].Input = append([]byte(nil), block.Input...)
	}
	return result
}
