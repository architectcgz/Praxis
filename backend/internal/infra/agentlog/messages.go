package agentlog

import (
	"praxis/internal/contracts"

	"context"
	"encoding/json"
	"errors"
	"fmt"

	appcontext "praxis/internal/context"

	runtimecontract "praxis/internal/runtime"
)

// ListMessages returns transcript messages in chronological order. A positive
// limit applies a UI page bound; zero reads the complete Agent transcript for
// runtime turn reconstruction.
func (s *Store) ListMessages(
	ctx context.Context,
	limit int,
) ([]runtimecontract.AgentSessionMessage, error) {
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

// LoadTranscript 读取同一时刻的完整 Agent transcript 和对应 sequence 边界。
func (s *Store) LoadTranscript(ctx context.Context) (runtimecontract.AgentTranscript, error) {
	if ctx == nil {
		return runtimecontract.AgentTranscript{}, errors.New("agent transcript load context is required")
	}
	if err := ctx.Err(); err != nil {
		return runtimecontract.AgentTranscript{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.entriesLocked()
	if err != nil {
		return runtimecontract.AgentTranscript{}, err
	}
	messages, err := messagesFromEntries(entries, 0)
	if err != nil {
		return runtimecontract.AgentTranscript{}, err
	}
	throughSequence := s.lastSequence
	if throughSequence == 0 {
		// 首次发送会先构建 Context，再由 runtime 写入 session header；为这段
		// 空 transcript 预留 header 的 sequence，才能通过 execution 输入校验。
		throughSequence = 1
	}
	return runtimecontract.AgentTranscript{
		ThroughSequence: throughSequence, Messages: messages,
	}, nil
}

func messagesFromEntries(entries []entry, limit int) ([]runtimecontract.AgentSessionMessage, error) {
	messages := make([]runtimecontract.AgentSessionMessage, 0)
	for _, value := range entries {
		if value.Kind != entryMessage {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message %d: %w", value.Sequence, err)
		}
		messages = append(messages, runtimecontract.AgentSessionMessage{
			Sequence:    value.Sequence,
			At:          value.At,
			ExecutionID: contracts.AgentExecutionID(value.ExecutionID),
			MessageID:   payload.MessageID,
			Digest:      payload.PayloadDigest,
			Role:        payload.Role,
			Content:     payload.Content,
			Thinking:    payload.Thinking,
			Blocks:      cloneBlocks(contextBlocksFromTranscript(payload.Blocks)),
		})
	}
	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// ListExecutionMessages returns messages already persisted by the current execution.
func (s *Store) ListExecutionMessages(ctx context.Context, currentExecutionID contracts.AgentExecutionID) ([]runtimecontract.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("execution transcript query context is required")
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
	messages := make([]runtimecontract.AgentSessionMessage, 0)
	for _, value := range entries {
		if value.Kind != entryMessage || value.ExecutionID != currentExecutionID.String() {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(value.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode transcript message %d: %w", value.Sequence, err)
		}
		messages = append(messages, runtimecontract.AgentSessionMessage{
			Sequence: value.Sequence, At: value.At,
			ExecutionID: contracts.AgentExecutionID(value.ExecutionID),
			MessageID:   payload.MessageID, Digest: payload.PayloadDigest,
			Role: payload.Role, Content: payload.Content, Thinking: payload.Thinking, Blocks: cloneBlocks(contextBlocksFromTranscript(payload.Blocks)),
		})
	}
	return messages, nil
}

func cloneBlocks(blocks []appcontext.ContextBlock) []appcontext.ContextBlock {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]appcontext.ContextBlock, len(blocks))
	for i, block := range blocks {
		cloned[i] = block.Clone()
	}
	return cloned
}
