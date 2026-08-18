package runtimetest

import (
	"context"
	"encoding/json"
	"sync"

	"praxis/internal/agentruntime"
)

// MemorySessionStore is a thread-safe append-only session store for local runtime tests.
type MemorySessionStore struct {
	mu          sync.Mutex
	entries     []agentruntime.AgentSessionEntry
	AppendError error
	FlushError  error
}

// Append implements agentruntime.AgentSessionStore.
func (s *MemorySessionStore) Append(_ context.Context, entries []agentruntime.AgentSessionEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AppendError != nil {
		return s.AppendError
	}
	for _, entry := range entries {
		entry.Payload = append([]byte(nil), entry.Payload...)
		s.entries = append(s.entries, entry)
	}
	return nil
}

// Flush implements agentruntime.SessionFlusher.
func (s *MemorySessionStore) Flush(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.FlushError
}

// Entries returns defensive copies of the durable entries.
func (s *MemorySessionStore) Entries() []agentruntime.AgentSessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]agentruntime.AgentSessionEntry, len(s.entries))
	for i, entry := range s.entries {
		result[i] = entry
		result[i].Payload = append([]byte(nil), entry.Payload...)
	}
	return result
}

// ReadContext implements agentruntime.AgentSessionStore.
func (s *MemorySessionStore) ReadContext(_ context.Context, _ string) (agentruntime.AgentSessionContext, error) {
	entries := s.Entries()
	projection := agentruntime.AgentSessionContext{Entries: entries}
	for _, entry := range entries {
		if entry.Sequence > projection.LastSequence {
			projection.LastSequence = entry.Sequence
		}
		if entry.Kind != agentruntime.EntryMessage {
			continue
		}
		var payload struct {
			Role    agentruntime.MessageRole    `json:"role"`
			Content []agentruntime.ContentBlock `json:"content"`
		}
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			continue
		}
		projection.Messages = append(projection.Messages, agentruntime.Message{Role: payload.Role, Content: payload.Content})
	}
	return projection, nil
}

// FindArtifact implements agentruntime.AgentSessionStore for idempotency checks.
func (s *MemorySessionStore) FindArtifact(_ context.Context, _ string, key string) (*agentruntime.SessionEntryRef, error) {
	entries := s.Entries()
	for _, entry := range entries {
		if entry.Kind != agentruntime.EntryContextArtifact {
			continue
		}
		var payload struct {
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if json.Unmarshal(entry.Payload, &payload) == nil && payload.IdempotencyKey == key {
			return &agentruntime.SessionEntryRef{ID: entry.ID, Sequence: entry.Sequence}, nil
		}
	}
	return nil, nil
}
