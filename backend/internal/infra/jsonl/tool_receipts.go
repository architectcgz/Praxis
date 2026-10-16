package jsonl

import (
	"cmp"
	"context"
	"errors"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/repository"
)

type receiptKey struct{ scope, owner, task, call string }

// prepareReceiptChanges 只校验调用声明和回执配对，不决定业务恢复或工具重试。
func (s *Store) prepareReceiptChanges(ctx context.Context) error {
	tx := s.tx(ctx)
	tx.receiptChanges = make(map[receiptKey]*repository.MissingToolResult)
	pending := func(key receiptKey) (repository.MissingToolResult, bool) {
		if value, changed := tx.receiptChanges[key]; changed {
			if value == nil {
				return repository.MissingToolResult{}, false
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
			if block.Kind != sessionmodel.BlockToolCall && block.Kind != sessionmodel.BlockToolResult {
				continue
			}
			key := receiptKey{
				tx.scope,
				record.OwnerID,
				record.Data.TaskID,
				block.CallID,
			}
			invocation, err := getByClaim[toolmodel.ToolInvocation](ctx, s, lookupKey{kind: "tool-call", owner: key.task, value: key.call})
			if err != nil {
				return err
			}
			if invocation.TurnID.String() != record.Data.TurnID || string(invocation.Name) != block.Name {
				return contracts.ErrRequestConflict
			}
			switch block.Kind {
			case sessionmodel.BlockToolCall:
				if _, exists := pending(key); exists {
					return contracts.ErrRequestConflict
				}
				if _, exists := s.index.latest[entityKey{"tool", invocation.ID.String()}]; exists {
					return errors.New("工具调用声明必须与新调用身份一起提交")
				}
				if invocation.AgentID.String() != record.Data.AuthorID || invocation.ArgumentsDigest != toolmodel.ArgumentsDigest(invocation.Name, toolCall(block).Arguments) {
					return contracts.ErrRequestConflict
				}
				fact := repository.MissingToolResult{
					SessionID: invocation.SessionID,
					AgentID:   invocation.AgentID,
					TaskID:    invocation.TaskID,
					TurnID:    invocation.TurnID,
					CallID:    block.CallID,
					RequestID: record.Data.RequestID,
					Name:      block.Name,
					Sequence:  event.Sequence,
					CallIndex: callIndex,
				}
				tx.receiptChanges[key] = &fact
				callIndex++
			case sessionmodel.BlockToolResult:
				fact, exists := pending(key)
				if !exists || fact.TurnID != invocation.TurnID || fact.Name != block.Name {
					return errors.New("工具回执缺少先前提交的调用声明")
				}
				if !invocation.Status.Terminal() {
					return errors.New("工具回执必须对应已结算调用")
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
		var invocation toolmodel.ToolInvocation
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
				if block.Kind == sessionmodel.BlockToolCall && record.Data.TaskID == invocation.TaskID.String() && record.Data.TurnID == invocation.TurnID.String() && block.CallID == invocation.ProviderToolCallID {
					declared = true
				}
			}
		}
		if !declared {
			return errors.New("新工具调用身份缺少同事务提交的 Assistant 声明")
		}
	}
	return nil
}

func (r MessageStreams) ListMissingToolResults(ctx context.Context) ([]repository.MissingToolResult, error) {
	values := make([]repository.MissingToolResult, 0)
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
	return sorted(values, func(a, b repository.MissingToolResult) int {
		return cmp.Or(cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(a.CallIndex, b.CallIndex))
	}), err
}
