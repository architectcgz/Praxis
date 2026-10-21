package storage

import (
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"slices"
)

// validateToolReceipts 检查候选消息与已提交调用的配对、声明顺序和归属。
func (s *Store) validateToolReceipts(entry stateEntry) error {
	var result messageRecord
	if err := decodeJSON(entry.payload, &result); err != nil {
		return err
	}
	for _, block := range result.Data.Blocks {
		if block.Kind != messagemodel.BlockToolResult {
			continue
		}
		declaration, err := s.lookup(lookupKey{kind: "tool-call", owner: result.Data.ExecutionID, value: block.CallID})
		if err != nil {
			return errors.New("tool receipt has no matching call declaration")
		}
		declared, ok := s.entry(declaration)
		if !ok {
			return fault.ErrNotFound
		}
		var call messageRecord
		if err := decodeJSON(declared.payload, &call); err != nil {
			return err
		}
		if declared.scope != entry.scope || declared.eventSequence >= entry.eventSequence || call.OwnerID != result.OwnerID || call.AgentID != result.AgentID || call.Data.RequestID != result.Data.RequestID || call.Data.TurnID != result.Data.TurnID {
			return errors.New("tool receipt ownership or declaration order mismatch")
		}
		if !slices.ContainsFunc(call.Data.Blocks, func(candidate messagemodel.Block) bool {
			return candidate.Kind == messagemodel.BlockToolCall && candidate.CallID == block.CallID && candidate.Name == block.Name
		}) {
			return errors.New("tool receipt name does not match its call declaration")
		}
	}
	return nil
}
