package agentlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"

	sessionport "praxis/internal/session"
)

// ListMessages returns transcript messages in chronological order. A positive
// limit applies a UI page bound; zero reads the complete Agent transcript for
// runtime turn reconstruction.
func (s *Store) ListMessages(
	ctx context.Context,
	limit int,
) ([]sessionport.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent session message context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	return messagesFromEntries(entries, limit)
}

// SnapshotContext atomically returns the current transcript input boundary.
func (s *Store) SnapshotContext(ctx context.Context) (sessionport.AgentTranscriptSnapshot, error) {
	if ctx == nil {
		return sessionport.AgentTranscriptSnapshot{}, errors.New("agent transcript snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return sessionport.AgentTranscriptSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return sessionport.AgentTranscriptSnapshot{}, err
	}
	messages, err := messagesFromEntries(entries, 0)
	if err != nil {
		return sessionport.AgentTranscriptSnapshot{}, err
	}
	artifacts, err := contextArtifactsFromEntries(entries)
	if err != nil {
		return sessionport.AgentTranscriptSnapshot{}, err
	}
	return sessionport.AgentTranscriptSnapshot{
		ThroughSequence: s.lastSequence, Messages: messages, Artifacts: artifacts,
	}, nil
}

func messagesFromEntries(entries []entry, limit int) ([]sessionport.AgentSessionMessage, error) {
	messages := make([]sessionport.AgentSessionMessage, 0)
	for _, value := range entries {
		if value.Kind != entryMessage {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message %d: %w", value.Sequence, err)
		}
		messages = append(messages, sessionport.AgentSessionMessage{
			Sequence:    value.Sequence,
			At:          value.At,
			ExecutionID: domainfoundation.AgentExecutionID(value.ExecutionID),
			MessageID:   payload.MessageID,
			Digest:      payload.PayloadDigest,
			Role:        payload.Role,
			Content:     payload.Content,
			Blocks:      cloneBlocks(payload.Blocks),
		})
	}
	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// ListExecutionMessages returns the frozen historical messages plus messages
// appended by the current execution, validating every frozen identity.
func (s *Store) ListExecutionMessages(ctx context.Context, references []domainexecution.TranscriptMessageRef, currentExecutionID domainfoundation.AgentExecutionID) ([]sessionport.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("execution transcript query context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make(map[uint64]domainexecution.TranscriptMessageRef, len(references))
	for _, reference := range references {
		wanted[reference.Sequence] = reference
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	messages := make([]sessionport.AgentSessionMessage, 0, len(references))
	found := 0
	for _, value := range entries {
		reference, selected := wanted[value.Sequence]
		current := value.Kind == entryMessage && value.ExecutionID == currentExecutionID.String()
		if !selected && !current {
			continue
		}
		if value.Kind != entryMessage {
			return nil, fmt.Errorf("selected transcript sequence %d is not a message", value.Sequence)
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message %d: %w", value.Sequence, err)
		}
		if selected {
			if value.ExecutionID != reference.ExecutionID.String() || payload.MessageID != reference.MessageID || payload.PayloadDigest != reference.Digest {
				return nil, fmt.Errorf("selected transcript message %d does not match its snapshot", value.Sequence)
			}
			found++
		}
		messages = append(messages, sessionport.AgentSessionMessage{
			Sequence: value.Sequence, At: value.At,
			ExecutionID: domainfoundation.AgentExecutionID(value.ExecutionID),
			MessageID:   payload.MessageID, Digest: payload.PayloadDigest,
			Role: payload.Role, Content: payload.Content, Blocks: cloneBlocks(payload.Blocks),
		})
	}
	if found != len(references) {
		return nil, errors.New("selected transcript message was not found")
	}
	return messages, nil
}

func cloneBlocks(blocks []sessionport.TranscriptContentBlock) []sessionport.TranscriptContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]sessionport.TranscriptContentBlock, len(blocks))
	for i, block := range blocks {
		cloned[i] = block
		cloned[i].Input = append([]byte(nil), block.Input...)
	}
	return cloned
}

// ListContextArtifacts returns artifacts matching the supplied immutable entry IDs.
func (s *Store) ListContextArtifacts(
	ctx context.Context,
	entryIDs []string,
) ([]sessionport.AgentContextArtifact, error) {
	if ctx == nil {
		return nil, errors.New("agent context artifact query context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(entryIDs) == 0 {
		return nil, nil
	}
	wanted := make(map[string]struct{}, len(entryIDs))
	for _, id := range entryIDs {
		if id == "" {
			return nil, errors.New("agent context artifact entry id is required")
		}
		wanted[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return nil, err
	}
	found := make(map[string]sessionport.AgentContextArtifact, len(entryIDs))
	for _, value := range entries {
		if value.Kind != entryArtifact {
			continue
		}
		if _, ok := wanted[value.ID]; !ok {
			continue
		}
		artifact, err := contextArtifactFromEntry(value)
		if err != nil {
			return nil, err
		}
		found[value.ID] = artifact
	}
	result := make([]sessionport.AgentContextArtifact, 0, len(entryIDs))
	for _, id := range entryIDs {
		artifact, ok := found[id]
		if !ok {
			return nil, fmt.Errorf("context artifact entry %s was not found", id)
		}
		result = append(result, artifact)
	}
	return result, nil
}

func contextArtifactsFromEntries(entries []entry) ([]sessionport.AgentContextArtifact, error) {
	artifacts := make([]sessionport.AgentContextArtifact, 0)
	for _, value := range entries {
		if value.Kind != entryArtifact {
			continue
		}
		artifact, err := contextArtifactFromEntry(value)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func contextArtifactFromEntry(value entry) (sessionport.AgentContextArtifact, error) {
	var payload artifactPayload
	if err := json.Unmarshal(value.Payload, &payload); err != nil {
		return sessionport.AgentContextArtifact{}, fmt.Errorf("decode context artifact %s: %w", value.ID, err)
	}
	return sessionport.AgentContextArtifact{
		EntryID: value.ID, Sequence: value.Sequence,
		DeliveryID: domainfoundation.DeliveryID(payload.DeliveryID), Kind: payload.Kind,
		ArtifactID: payload.ArtifactID, Body: append(json.RawMessage(nil), payload.Body...),
	}, nil
}
