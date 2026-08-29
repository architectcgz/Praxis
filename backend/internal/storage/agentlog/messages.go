package agentlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

// ListMessages returns the latest transcript messages in chronological order.
// The read is intentionally separate from TranscriptReceiptStore so runtime code
// cannot use the UI projection to influence execution context or state.
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
	if limit <= 0 {
		limit = 100
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
			ExecutionID: domain.AgentExecutionID(value.ExecutionID),
			Role:        payload.Role,
			Content:     payload.Content,
		})
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}
