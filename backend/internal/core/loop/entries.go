package loop

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
)

// appendEntry 将 Main 条目接纳到共享内存仓储，子执行只更新本次历史；两者都不等待落盘。
func (r *Runner) appendEntry(ctx context.Context, input Input, value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	if input.Runtime.Main() {
		stored, inserted, err := r.Entries.Append(ctx, sessionentry.Record{
			SessionID: input.SessionID,
			AgentID:   input.AgentID,
			Data:      value,
		})
		if err != nil {
			return sessionentry.ContextEntry{}, false, err
		}
		accepted, _, err := input.Runtime.Accept(stored)
		return accepted, inserted, err
	}
	return input.Runtime.Accept(value)
}

func entryFailure(input Input, err error) error {
	if input.Runtime.Main() {
		return failCause(FailureStorage, err)
	}
	return failCause(FailureContract, err)
}
