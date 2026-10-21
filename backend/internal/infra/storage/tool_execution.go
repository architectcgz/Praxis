package storage

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"slices"
)

type ToolExecutionRepository struct{ s *Store }

// Read 按日志序号读取控制事件；投影仅在本次查询中存在。
func (r ToolExecutionRepository) Read(ctx context.Context, key toolexecution.Key) (facts toolexecution.Facts, err error) {
	err = r.s.view(ctx, func(readCtx context.Context) error {
		keys := r.s.keys(readCtx, lookupKey{kind: "tool-events", owner: key.ExecutionID.String(), value: key.CallID})
		slices.SortFunc(keys, func(a, b entityKey) int {
			left, _ := r.s.entry(readCtx, a)
			right, _ := r.s.entry(readCtx, b)
			if left.eventSequence < right.eventSequence {
				return -1
			}
			if left.eventSequence > right.eventSequence {
				return 1
			}
			return 0
		})
		for _, key := range keys {
			data, err := r.s.payload(readCtx, key)
			if err != nil {
				return err
			}
			var event toolexecution.Event
			if err := decodeJSON(data, &event); err != nil {
				return err
			}
			if err := facts.Apply(event); err != nil {
				return err
			}
		}
		return nil
	})
	return facts, err
}

// Append 的事务不包裹工具调用；started 的唯一性由事件投影约束。
func (r ToolExecutionRepository) Append(ctx context.Context, event toolexecution.Event) error {
	return r.s.InTx(ctx, func(txCtx context.Context) error {
		call, err := (ToolCallQueries{r.s}).GetCall(txCtx, event.Key)
		if err != nil {
			return err
		}
		if event.Kind == toolexecution.Approved || event.Kind == toolexecution.ApprovalRequired {
			if toolexecution.ArgumentsDigest(call.Name, []byte(event.Summary)) != event.ArgumentsDigest {
				return fault.ErrRequestConflict
			}
		}
		if call.SessionID != event.SessionID {
			return fault.ErrRequestConflict
		}
		if _, err := (ToolCallQueries{r.s}).GetResult(txCtx, event.Key); err == nil {
			return fault.ErrRequestConflict
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		facts, err := r.Read(txCtx, event.Key)
		if err != nil {
			return err
		}
		if err := facts.Apply(event); err != nil {
			return err
		}
		tx := r.s.tx(txCtx)
		sequence := r.s.log.Sequence(event.SessionID.String()) + 1
		if len(tx.events) > 0 {
			sequence = tx.nextSequence + 1
		}
		return save(txCtx, r.s, event.SessionID.String(), "tool_event", fmt.Sprintf("%s@%d", event.Key, sequence), string(event.Kind), event)
	})
}
