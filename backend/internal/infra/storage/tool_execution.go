package storage

import (
	"cmp"
	"context"
	"fmt"
	"praxis/internal/core/toolexecution"
	"slices"
)

type ToolExecutionRepository struct{ s *Store }

// Read 按日志序号读取控制事件；投影仅在本次查询中存在。
func (r ToolExecutionRepository) Read(ctx context.Context, key toolexecution.Key) (facts toolexecution.Facts, err error) {
	err = r.s.view(ctx, func() error {
		facts, err = r.readLocked(key)
		return err
	})
	return facts, err
}

func (r ToolExecutionRepository) readLocked(key toolexecution.Key) (toolexecution.Facts, error) {
	var facts toolexecution.Facts
	keys := r.s.keys(lookupKey{kind: "tool-events", owner: key.ExecutionID.String(), value: key.CallID})
	slices.SortFunc(keys, func(a, b entityKey) int {
		left, _ := r.s.entry(a)
		right, _ := r.s.entry(b)
		return cmp.Compare(left.eventSequence, right.eventSequence)
	})
	for _, key := range keys {
		data, err := r.s.payload(key)
		if err != nil {
			return facts, err
		}
		var event toolexecution.Event
		if err := decodeJSON(data, &event); err != nil {
			return facts, err
		}
		facts.Apply(event)
	}
	return facts, nil
}

// Append 独立提交一个控制事件，工具调用不在存储锁内执行。
func (r ToolExecutionRepository) Append(ctx context.Context, event toolexecution.Event) error {
	return r.s.view(ctx, func() error {
		sequence := r.s.log.Sequence(event.SessionID.String()) + 1
		return r.s.appendLocked(ctx, event.SessionID.String(), "tool_event", fmt.Sprintf("%s@%d", event.Key, sequence), string(event.Kind), event)
	})
}
