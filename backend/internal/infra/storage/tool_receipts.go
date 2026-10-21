package storage

import (
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
)

// validateToolReceipts 校验通用索引派生的配对、声明顺序与归属，不更新 Execution。
func (s *Store) validateToolReceipts(ctx context.Context) error {
	for key, change := range s.tx(ctx).changes {
		if key.collection != "message" {
			continue
		}
		var result messageRecord
		if err := decodeJSON(change.entry.payload, &result); err != nil {
			return err
		}
		for _, block := range result.Data.Blocks {
			if block.Kind != messagemodel.BlockToolResult {
				continue
			}
			declaration, err := s.lookup(ctx, lookupKey{kind: "tool-call", owner: result.Data.ExecutionID, value: block.CallID})
			if err != nil {
				return errors.New("tool receipt has no matching call declaration")
			}
			entry, ok := s.entry(ctx, declaration)
			if !ok {
				return fault.ErrNotFound
			}
			var call messageRecord
			if err := decodeJSON(entry.payload, &call); err != nil {
				return err
			}
			if entry.scope != change.entry.scope || entry.eventSequence >= change.entry.eventSequence || call.OwnerID != result.OwnerID || call.AgentID != result.AgentID || call.Data.RequestID != result.Data.RequestID || call.Data.TurnID != result.Data.TurnID {
				return errors.New("tool receipt ownership or declaration order mismatch")
			}
			matched := false
			for _, declared := range call.Data.Blocks {
				if declared.Kind == messagemodel.BlockToolCall && declared.CallID == block.CallID && declared.Name == block.Name {
					matched = true
					break
				}
			}
			if !matched {
				return errors.New("tool receipt name does not match its call declaration")
			}
		}
	}
	return nil
}
