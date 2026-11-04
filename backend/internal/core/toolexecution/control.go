package toolexecution

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
)

func (e *Executor) register(ctx context.Context, call sessionentry.ToolCall, execution *Execution) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.checkUnfinished(ctx, call); err != nil {
		return err
	}
	e.active[call.Key].tool = execution
	return nil
}

func (e *Executor) unregister(key Key) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.active, key)
}

// StopCall 只提交停止信号；尚未启动的调用也由所属执行路径生成唯一回执。
func (e *Executor) StopCall(ctx context.Context, request ControlRequest) (agentrun.RunRecord, error) {
	if err := ctx.Err(); err != nil {
		return agentrun.RunRecord{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	callState := e.active[Key{RunID: request.RunID, CallID: request.CallID}]
	if callState == nil {
		return agentrun.RunRecord{}, errorcode.ErrRequestConflict
	}
	callState.stopped = true
	if callState.tool != nil {
		callState.tool.Stop()
	}
	return callState.owner, nil
}

// DecideApproval 只更新当前活跃调用，迟到决定不能恢复审批或作用于其他执行。
func (e *Executor) DecideApproval(ctx context.Context, request ControlRequest, approve bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	callState := e.active[Key{RunID: request.RunID, CallID: request.CallID}]
	if callState == nil {
		return errorcode.ErrRequestConflict
	}
	if callState.stopped || callState.request == nil || callState.tool != nil {
		return errorcode.ErrRequestConflict
	}
	if callState.decision != nil {
		if callState.decision.Approved != approve {
			return errorcode.ErrRequestConflict
		}
		return nil
	}
	callState.decision = &UserDecision{
		Key:       callState.call.Key,
		SessionID: callState.call.SessionID,
		Approved:  approve,
		DecidedAt: e.deps.Clock.Now().UTC(),
	}
	return nil
}
