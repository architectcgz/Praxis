package toolexecution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
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

func (e *Executor) resolveActive(request ControlRequest) (*callState, error) {
	callState := e.active[Key{ExecutionID: request.ExecutionID, CallID: request.CallID}]
	if callState == nil || request.SessionID != callState.call.SessionID || request.AgentID != callState.call.AgentID {
		return nil, errorcode.ErrRequestConflict
	}
	return callState, nil
}

// StopCall 只提交停止信号；尚未启动的调用也由所属执行路径生成唯一回执。
func (e *Executor) StopCall(ctx context.Context, request ControlRequest) (agentexecution.Execution, error) {
	if err := ctx.Err(); err != nil {
		return agentexecution.Execution{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	callState, err := e.resolveActive(request)
	if err != nil {
		return agentexecution.Execution{}, err
	}
	callState.stopped = true
	if callState.tool != nil {
		callState.tool.Stop()
	}
	return callState.owner, nil
}

// DecideApproval 只更新当前活跃调用，迟到决定不能恢复审批或作用于其他执行。
func (e *Executor) DecideApproval(ctx context.Context, request ControlRequest, approve bool) (agentexecution.Execution, error) {
	if err := ctx.Err(); err != nil {
		return agentexecution.Execution{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	callState, err := e.resolveActive(request)
	if err != nil {
		return agentexecution.Execution{}, err
	}
	if callState.stopped || callState.request == nil || callState.tool != nil {
		return callState.owner, errorcode.ErrRequestConflict
	}
	if callState.decision != nil {
		if callState.decision.Approved != approve {
			return callState.owner, errorcode.ErrRequestConflict
		}
		return callState.owner, nil
	}
	callState.decision = &UserDecision{
		Key:       callState.call.Key,
		SessionID: callState.call.SessionID,
		Approved:  approve,
		DecidedAt: e.deps.Clock.Now().UTC(),
	}
	return callState.owner, nil
}
