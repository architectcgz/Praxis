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

func (e *Executor) resolveActive(request ControlRequest) (*activeCall, error) {
	active := e.active[Key{ExecutionID: request.ExecutionID, CallID: request.CallID}]
	if active == nil || request.SessionID != active.call.SessionID || request.AgentID != active.call.AgentID {
		return nil, errorcode.ErrRequestConflict
	}
	return active, nil
}

// StopCall 只提交停止信号；尚未启动的调用也由所属执行路径生成唯一回执。
func (e *Executor) StopCall(ctx context.Context, request ControlRequest) (agentexecution.Execution, error) {
	if err := ctx.Err(); err != nil {
		return agentexecution.Execution{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	active, err := e.resolveActive(request)
	if err != nil {
		return agentexecution.Execution{}, err
	}
	active.stopped = true
	if active.tool != nil {
		active.tool.Stop()
	}
	return active.owner, nil
}

// DecideApproval 只更新当前活跃调用，迟到决定不能恢复审批或作用于其他执行。
func (e *Executor) DecideApproval(ctx context.Context, request ControlRequest, approve bool) (agentexecution.Execution, error) {
	if err := ctx.Err(); err != nil {
		return agentexecution.Execution{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	active, err := e.resolveActive(request)
	if err != nil {
		return agentexecution.Execution{}, err
	}
	if active.stopped || active.request == nil || active.tool != nil {
		return active.owner, errorcode.ErrRequestConflict
	}
	if active.decision != nil {
		if active.decision.Approved != approve {
			return active.owner, errorcode.ErrRequestConflict
		}
		return active.owner, nil
	}
	active.decision = &UserDecision{
		Key:       active.call.Key,
		SessionID: active.call.SessionID,
		Approved:  approve,
		DecidedAt: e.deps.Clock.Now().UTC(),
	}
	return active.owner, nil
}
