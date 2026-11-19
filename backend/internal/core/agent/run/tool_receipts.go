package run

import (
	"context"
	"fmt"
	"praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
)

// closeToolCalls 在失败收尾前补齐已接纳声明的回执，不重新执行工具。
// 日志尚未确认完整时 AppendEntry 直接返回保存错误，此时不能发布 Run 终态。
func (r *Run) closeToolCalls(ctx context.Context) error {
	values := r.Entries()
	completed := make(map[string]bool)
	for _, e := range values {
		_, blocks := e.Dialogue()
		for _, b := range blocks {
			if result, ok := b.(entry.ToolResultBlock); ok {
				completed[result.CallID] = true
			}
		}
	}
	for _, e := range values {
		var results []entry.ContentBlock
		_, blocks := e.Dialogue()
		for _, b := range blocks {
			call, ok := b.(entry.ToolCallBlock)
			if !ok || completed[call.CallID] {
				continue
			}
			results = append(results, entry.ToolResultBlock{
				CallID:  call.CallID,
				Name:    call.Name,
				Text:    "The execution ended before a reliable tool result was available.",
				IsError: true,
			})
		}
		if len(results) == 0 {
			continue
		}
		stored, _, err := r.AppendEntry(ctx, entry.Entry{
			ID:         "tools:" + e.TurnID,
			TurnID:     e.TurnID,
			AuthorKind: entry.AuthorTool,
			Payload: entry.Message{
				Role:    role.Tool,
				Content: results,
			},
		})
		if err != nil {
			return fmt.Errorf("close tool receipts: %w", err)
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(identity.TurnID(e.TurnID)),
			Payload: coreevent.EntriesAppended{Entries: []entry.Entry{*stored}},
		})
	}
	return nil
}
