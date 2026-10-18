package jsonl

import (
	"cmp"
	"context"
	"errors"
	"praxis/internal/fault"
	"praxis/internal/identity"
	messagemodel "praxis/internal/message"
	"praxis/internal/toolcall"
)

type receiptKey struct{ scope, owner, task, call string }

// prepareReceiptChanges 只校验调用声明和回执配对，不决定业务恢复或工具重试。
func (s *Store) prepareReceiptChanges(ctx context.Context) error {
	tx := s.tx(ctx)
	tx.receiptChanges = make(map[receiptKey]*messagemodel.MissingToolResult)
	pending := func(key receiptKey) (messagemodel.MissingToolResult, bool) {
		if value, changed := tx.receiptChanges[key]; changed {
			if value == nil {
				return messagemodel.MissingToolResult{}, false
			}
			return *value, true
		}
		value, ok := s.index.missing[key]
		return value, ok
	}
	for _, event := range tx.events {
		if event.Collection != "message" {
			continue
		}
		var record messageRecord
		if err := decodeJSON(event.Payload, &record); err != nil {
			return err
		}
		callIndex := 0
		for _, block := range record.Data.Blocks {
			if block.Kind != messagemodel.BlockToolCall && block.Kind != messagemodel.BlockToolResult {
				continue
			}
			key := receiptKey{
				tx.scope,
				record.OwnerID,
				record.Data.TaskID,
				block.CallID,
			}
			invocation, err := getByClaim[toolcall.Execution](ctx, s, lookupKey{kind: "tool-call", owner: key.task, value: key.call})
			if err != nil {
				return err
			}
			if invocation.TurnID.String() != record.Data.TurnID || string(invocation.Name) != block.Name {
				return fault.ErrRequestConflict
			}
			switch block.Kind {
			case messagemodel.BlockToolCall:
				if _, exists := pending(key); exists {
					return fault.ErrRequestConflict
				}
				if _, exists := s.index.latest[entityKey{"tool", invocation.ID.String()}]; exists {
					return errors.New("tool call declaration must be committed with a new execution identity")
				}
				if invocation.AgentID.String() != record.Data.AuthorID || invocation.ArgumentsDigest != toolcall.ArgumentsDigest(invocation.Name, block.Input) {
					return fault.ErrRequestConflict
				}
				fact := messagemodel.MissingToolResult{
					SessionID: identity.SessionID(tx.scope),
					AgentID:   record.AgentID,
					TaskID:    identity.TaskID(record.Data.TaskID),
					TurnID:    identity.TurnID(record.Data.TurnID),
					CallID:    block.CallID,
					RequestID: record.Data.RequestID,
					Name:      block.Name,
					Sequence:  event.Sequence,
					CallIndex: callIndex,
				}
				tx.receiptChanges[key] = &fact
				callIndex++
			case messagemodel.BlockToolResult:
				fact, exists := pending(key)
				if !exists || fact.TurnID.String() != record.Data.TurnID || fact.Name != block.Name || fact.AgentID != record.AgentID {
					return errors.New("tool receipt has no previously committed call declaration")
				}
				if !invocation.Status.Terminal() {
					return errors.New("tool receipt must correspond to a settled execution")
				}
				tx.receiptChanges[key] = nil
			}
		}
	}
	for key, change := range tx.changes {
		if key.collection != "tool" {
			continue
		}
		if _, exists := s.index.latest[key]; exists {
			continue
		}
		var invocation toolcall.Execution
		if err := decodeJSON(change.event.Payload, &invocation); err != nil {
			return err
		}
		declared := false
		for _, event := range tx.events {
			if event.Collection != "message" {
				continue
			}
			var record messageRecord
			if err := decodeJSON(event.Payload, &record); err != nil {
				return err
			}
			for _, block := range record.Data.Blocks {
				if block.Kind == messagemodel.BlockToolCall && record.Data.TaskID == invocation.TaskID.String() && record.Data.TurnID == invocation.TurnID.String() && block.CallID == invocation.ProviderToolCallID {
					declared = true
				}
			}
		}
		if !declared {
			return errors.New("new tool execution identity has no assistant declaration in the same transaction")
		}
	}
	return nil
}

func (r MessageStreams) ListMissingToolResults(ctx context.Context) ([]messagemodel.MissingToolResult, error) {
	values := make([]messagemodel.MissingToolResult, 0)
	err := r.s.view(ctx, func(readCtx context.Context) error {
		tx := r.s.tx(readCtx)
		if tx.deletedScope {
			for key, value := range r.s.index.missing {
				if key.scope != tx.scope {
					values = append(values, value)
				}
			}
			return nil
		}
		// 事务内恢复查询也要看到本事务新增的声明及已追加的回执。
		if len(tx.events) > 0 {
			if err := r.s.prepareReceiptChanges(readCtx); err != nil {
				return err
			}
		}
		for key, value := range r.s.index.missing {
			if _, changed := tx.receiptChanges[key]; !changed {
				values = append(values, value)
			}
		}
		for _, value := range tx.receiptChanges {
			if value != nil {
				values = append(values, *value)
			}
		}
		return nil
	})
	return sorted(values, func(a, b messagemodel.MissingToolResult) int {
		return cmp.Or(cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(a.CallIndex, b.CallIndex))
	}), err
}
