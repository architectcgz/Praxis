package toolexecution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
)

// register 与 Stop 同步启动边界，防止已取消的调用继续启动。
func (e *Executor) register(ctx context.Context, call sessionentry.ToolCall, execution *Execution) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.checkUnfinished(ctx, call); err != nil {
		return err
	}
	e.active[call.Key] = execution
	return nil
}

func (e *Executor) unregister(key Key) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.active, key)
}

// StopCall 对运行中工具发送取消；尚未启动时以唯一回执阻止后续启动。
func (e *Executor) StopCall(ctx context.Context, request ControlRequest) (agentexecution.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	call, execution, err := e.deps.Operations.ResolveCall(ctx, request)
	if err != nil {
		return execution, err
	}
	if _, found, err := e.deps.Operations.GetResult(ctx, call.Key); err != nil || found {
		return execution, err
	}
	if !execution.Active() {
		return execution, errorcode.ErrRequestConflict
	}
	if active := e.active[call.Key]; active != nil {
		active.Stop()
		return execution, nil
	}
	_, err = e.commitResultLocked(ctx, call, tool.NewToolError("tool_interrupted", "The tool call was canceled before execution."))
	return execution, err
}

// DecideApproval 校验人工决定并保存事实；只唤醒当前 Loop，不重启执行生命周期。
func (e *Executor) DecideApproval(ctx context.Context, request ControlRequest, approve bool) (agentexecution.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	call, execution, err := e.deps.Operations.ResolveCall(ctx, request)
	if err != nil {
		return execution, err
	}
	approval, found, err := e.deps.Operations.GetApprovalRequest(ctx, call.Key)
	if err != nil {
		return execution, err
	}
	if !found {
		return execution, errorcode.ErrRequestConflict
	}
	previous, decided, err := e.deps.Operations.GetUserDecision(ctx, call.Key)
	if err != nil {
		return execution, err
	}
	// 完成后不再批准；同一次人工拒绝可重复提交，矛盾决定不能覆盖。
	if _, complete, err := e.deps.Operations.GetResult(ctx, call.Key); err != nil {
		return execution, err
	} else if complete {
		if !approve && decided && !previous.Approved {
			return execution, nil
		}
		return execution, errorcode.ErrRequestConflict
	}
	if !execution.Active() {
		return execution, errorcode.ErrRequestConflict
	}
	if decided {
		if previous.Approved != approve {
			return execution, errorcode.ErrRequestConflict
		}
		if approve {
			return execution, nil
		}
	}
	if !decided {
		at := e.deps.Clock.Now().UTC()
		if at.Before(approval.RequestedAt) {
			at = approval.RequestedAt
		}
		decision := UserDecision{
			Key:       call.Key,
			SessionID: call.SessionID,
			Approved:  approve,
			DecidedAt: at,
		}
		if err := e.deps.Operations.SaveAuthorization(ctx, Event{
			Key:          call.Key,
			SessionID:    call.SessionID,
			Kind:         ApprovalDecided,
			At:           at,
			UserDecision: &decision,
		}); err != nil {
			return execution, err
		}
	}

	// 用户决定可靠保存后即可唤醒；拒绝回执由当前工具流程提交，失败不会丢失唤醒。
	// 批准绑定当前调用，唤醒后沿用该调用最初读取的规则决定。
	return execution, err
}
