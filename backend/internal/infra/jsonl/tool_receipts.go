package jsonl

import (
	"cmp"
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

type receiptKey struct{ scope, owner, task, call string }

// callReference 只索引消息位置和配对身份，不缓存参数或结果正文。
type callReference struct {
	fact       repository.MissingToolResult
	owner      string
	messageID  string
	blockIndex int
	resultID   string
}

// prepareReceiptChanges 以消息声明与回执为唯一配对事实。
func (s *Store) prepareReceiptChanges(ctx context.Context) error {
	tx := s.tx(ctx)
	tx.receiptChanges = make(map[receiptKey]*repository.MissingToolResult)
	tx.callChanges = make(map[toolexecution.Key]callReference)
	for _, event := range tx.events {
		if event.Collection != "message" {
			continue
		}
		var record messageRecord
		if err := decodeJSON(event.Payload, &record); err != nil {
			return err
		}
		callIndex := 0
		for blockIndex, block := range record.Data.Blocks {
			if block.Kind != messagemodel.BlockToolCall && block.Kind != messagemodel.BlockToolResult {
				continue
			}
			if record.Data.ExecutionID == "" || record.Data.TurnID == "" {
				return errors.New("tool message ownership is required")
			}
			key := toolexecution.Key{ExecutionID: identity.AgentExecutionID(record.Data.ExecutionID), CallID: block.CallID}
			receipt := receiptKey{
				tx.scope,
				record.OwnerID,
				record.Data.ExecutionID,
				block.CallID,
			}
			prior, exists := tx.callChanges[key]
			if !exists {
				prior, exists = s.index.calls[key]
			}
			switch block.Kind {
			case messagemodel.BlockToolCall:
				if exists {
					return fault.ErrRequestConflict
				}
				fact := repository.MissingToolResult{
					SessionID:   identity.SessionID(tx.scope),
					AgentID:     record.AgentID,
					ExecutionID: key.ExecutionID,
					CallID:      key.CallID,
					RequestID:   record.Data.RequestID,
					TurnID:      identity.TurnID(record.Data.TurnID),
					Name:        block.Name,
					Sequence:    event.Sequence,
					CallIndex:   callIndex,
				}
				tx.callChanges[key] = callReference{
					fact:       fact,
					owner:      record.OwnerID,
					messageID:  record.Data.ID,
					blockIndex: blockIndex,
				}
				tx.receiptChanges[receipt] = &fact
				callIndex++
			case messagemodel.BlockToolResult:
				if !exists || prior.resultID != "" || prior.fact.SessionID.String() != tx.scope || prior.owner != record.OwnerID || prior.fact.AgentID != record.AgentID || prior.fact.TurnID.String() != record.Data.TurnID || prior.fact.RequestID != record.Data.RequestID || prior.fact.Name != block.Name {
					return errors.New("tool receipt has no matching unfinished call declaration")
				}
				prior.resultID = record.Data.ID
				tx.callChanges[key] = prior
				tx.receiptChanges[receipt] = nil
			}
		}
	}
	return nil
}

func (r MessageStreams) callReference(ctx context.Context, key toolexecution.Key) (callReference, error) {
	tx := r.s.tx(ctx)
	if len(tx.events) > 0 {
		if err := r.s.prepareReceiptChanges(ctx); err != nil {
			return callReference{}, err
		}
		if ref, ok := tx.callChanges[key]; ok {
			return ref, nil
		}
	}
	ref, ok := r.s.index.calls[key]
	if !ok || tx.deletedScope && ref.fact.SessionID.String() == tx.scope {
		return callReference{}, fault.ErrNotFound
	}
	return ref, nil
}

func (r MessageStreams) ToolCall(ctx context.Context, key toolexecution.Key) (result messagemodel.ToolCall, err error) {
	err = r.s.view(ctx, func(readCtx context.Context) error {
		ref, err := r.callReference(readCtx, key)
		if err != nil {
			return err
		}
		data, err := r.s.payload(readCtx, messageEntityKey(ref.fact.SessionID.String(), ref.owner, ref.messageID))
		if err != nil {
			return err
		}
		var record messageRecord
		if err := decodeJSON(data, &record); err != nil {
			return err
		}
		block := record.Data.Blocks[ref.blockIndex]
		result = messagemodel.ToolCall{
			Key:       key,
			SessionID: ref.fact.SessionID,
			AgentID:   ref.fact.AgentID,
			RequestID: ref.fact.RequestID,
			TurnID:    ref.fact.TurnID,
			Name:      tool.ToolName(block.Name),
			Arguments: block.Input,
			CreatedAt: record.Data.CreatedAt,
		}
		return nil
	})
	return result, err
}

func (r MessageStreams) ToolResult(ctx context.Context, key toolexecution.Key) (result messagemodel.Message, err error) {
	err = r.s.view(ctx, func(readCtx context.Context) error {
		ref, err := r.callReference(readCtx, key)
		if err != nil {
			return err
		}
		if ref.resultID == "" {
			return fault.ErrNotFound
		}
		data, err := r.s.payload(readCtx, messageEntityKey(ref.fact.SessionID.String(), ref.owner, ref.resultID))
		if err != nil {
			return err
		}
		var record messageRecord
		if err := decodeJSON(data, &record); err != nil {
			return err
		}
		result = record.Data
		return nil
	})
	return result, err
}

func (r MessageStreams) ListMissingToolResults(ctx context.Context) ([]repository.MissingToolResult, error) {
	values := make([]repository.MissingToolResult, 0)
	err := r.s.view(ctx, func(readCtx context.Context) error {
		tx := r.s.tx(readCtx)
		if len(tx.events) > 0 {
			if err := r.s.prepareReceiptChanges(readCtx); err != nil {
				return err
			}
		}
		for key, value := range r.s.index.missing {
			if tx.deletedScope && key.scope == tx.scope {
				continue
			}
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
