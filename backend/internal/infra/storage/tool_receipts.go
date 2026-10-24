package storage

import (
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"slices"
)

// validateToolReceipts 检查候选消息与已提交调用的配对、声明顺序和归属。
func (s *Store) validateToolReceipts(entry stateEntry) error {
	result := entry.entryValue()
	for _, block := range result.Data.Content {
		if block.Kind != sessionentry.BlockToolResult {
			continue
		}
		declaration, err := s.lookup(lookupKey{kind: "tool-call", owner: result.Data.ExecutionID, value: block.CallID})
		if err != nil {
			return errors.New("tool receipt has no matching call declaration")
		}
		declared, ok := s.entry(declaration)
		if !ok {
			return errorcode.ErrNotFound
		}
		call := declared.entryValue()
		if declared.scope != entry.scope || declared.eventSequence >= entry.eventSequence || call.AgentID != result.AgentID || call.Data.RequestID != result.Data.RequestID || call.Data.TurnID != result.Data.TurnID {
			return errors.New("tool receipt ownership or declaration order mismatch")
		}
		if !slices.ContainsFunc(call.Data.Content, func(candidate sessionentry.Block) bool {
			return candidate.Kind == sessionentry.BlockToolCall && candidate.CallID == block.CallID && candidate.Name == block.Name
		}) {
			return errors.New("tool receipt name does not match its call declaration")
		}
	}
	return nil
}
