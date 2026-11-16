package run

import (
	"context"
	sessionentry "praxis/internal/core/entry"
)

// AppendEntry 保存事实并接纳同一只读引用；通知和观察 Hook 由执行流程显式调用。
func (r *Run) AppendEntry(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	r.commitMu.Lock()
	defer r.commitMu.Unlock()
	value.RunID = r.Snapshot().ID.String()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = r.deps.Clock.Now()
	}
	stored, inserted, err := r.appendFact(ctx, value)
	if stored != nil && inserted {
		r.accept(stored)
	}
	if err != nil {
		return stored, inserted, storageFailure(err)
	}
	return stored, inserted, nil
}
