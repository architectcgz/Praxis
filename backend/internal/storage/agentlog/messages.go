package agentlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainfoundation "praxis/internal/core/domain/foundation"

	coresession "praxis/internal/core/session"
)

// ListMessages returns transcript messages in chronological order. A positive
// limit applies a UI page bound; zero reads the complete Agent transcript for
// runtime turn reconstruction.
func (s *Store) ListMessages(
	ctx context.Context,
	limit int,
) ([]coresession.AgentSessionMessage, error) {
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
	messages := make([]coresession.AgentSessionMessage, 0)
	for _, value := range entries {
		if value.Kind != entryMessage {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message %d: %w", value.Sequence, err)
		}
		messages = append(messages, coresession.AgentSessionMessage{
			Sequence:    value.Sequence,
			At:          value.At,
			ExecutionID: domainfoundation.AgentExecutionID(value.ExecutionID),
			MessageID:   payload.MessageID,
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

func cloneBlocks(blocks []coresession.TranscriptContentBlock) []coresession.TranscriptContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]coresession.TranscriptContentBlock, len(blocks))
	for i, block := range blocks {
		cloned[i] = block
		cloned[i].Input = append([]byte(nil), block.Input...)
	}
	return cloned
}
