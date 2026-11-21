package toolexecution

import (
	"context"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
)

func (e *Executor) register(key tool.CallKey, call *activeCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.active[key] = call
}

func (e *Executor) unregister(key tool.CallKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.active, key)
}

// StopCall 取消目标活跃工具调用的派生上下文，同时中断审核、审批等待或执行中的工具。
func (e *Executor) StopCall(ctx context.Context, request ControlRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	active := e.active[tool.CallKey{RunID: request.RunID, CallID: request.CallID}]
	if active == nil || active.sessionID != request.SessionID || active.agentID != request.AgentID {
		e.mu.Unlock()
		return errorcode.ErrRequestConflict
	}
	cancel := active.cancel
	e.mu.Unlock()
	cancel(errCallStopped)
	return nil
}
