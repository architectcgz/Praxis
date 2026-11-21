package run

import (
	"context"
	"fmt"
	"praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
)

// closeToolCalls 在失败收尾前补齐已接纳声明的回执，不重新执行工具。
// 日志尚未确认完整时 AppendEntry 直接返回保存错误，此时不能发布 Run 终态。
func (r *Run) closeToolCalls(ctx context.Context) error {
	for _, receipt := range entry.MissingToolResultEntries(r.entries, r.deps.Clock.Now()) {
		stored, _, err := r.AppendEntry(ctx, receipt)
		if err != nil {
			return fmt.Errorf("close tool receipts: %w", err)
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(identity.TurnID(receipt.TurnID)),
			Payload: coreevent.EntriesAppended{Entries: []*entry.Entry{stored}},
		})
	}
	return nil
}
