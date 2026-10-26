package toolexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

var errResultAvailable = errors.New("tool result is already committed")
var errCallStopped = errors.New("tool call was stopped")

type authorizationResult struct {
	pending bool
	denied  *tool.ToolResult
	review  bool
}

// authorize 完整调度规则判断、模型审核和人工审批，只向工具执行层返回最终授权结果。
// args 由执行流程冻结；规则只读取一次，审批唤醒后不重新匹配，审批与审核共享只读参数。
func (t *ExecutionTools) authorize(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage, command string, operationID string) (*tool.ToolResult, error) {
	e := t.executor
	decision, err := e.deps.Operations.DecideRule(ctx, call, command)
	if err != nil {
		return nil, err
	}
	for {
		e.mu.Lock()
		if err := e.checkUnfinished(ctx, call); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		callState := e.active[call.Key]
		result, err := e.evaluate(callState, decision)
		if err != nil {
			e.mu.Unlock()
			return nil, err
		}
		sourceTaskID := callState.owner.SourceTaskID
		if result.pending {
			if callState.owner.Status != agentexecution.ExecutionRunning {
				e.mu.Unlock()
				return nil, errorcode.ErrRequestConflict
			}
			// 判断与请求登记共享控制锁，通知前请求已可查询，不能与停止或用户决定交错修改。
			if callState.request == nil {
				callState.request = &ApprovalRequest{
					Key:         call.Key,
					SessionID:   call.SessionID,
					Arguments:   args,
					Decision:    decision,
					RequestedAt: e.deps.Clock.Now().UTC(),
				}
			}
		}
		e.mu.Unlock()

		// Hook、模型请求和人工等待均在控制锁外执行，控制入口只登记决定或停止并唤醒。
		if result.pending {
			e.notifyWaiting(ctx, call, args, operationID, sourceTaskID)
			if err := t.awaitApproval(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if !result.review {
			return result.denied, nil
		}
		request, err := e.deps.Operations.PrepareReview(ctx, t.run, call, t.workspacePath)
		if err != nil {
			return nil, err
		}
		request.Call, request.Arguments, request.Decision = call, args, decision
		reviewCtx, cancel := context.WithTimeout(ctx, time.Minute)
		assessment, reviewErr := e.deps.Reviewer.review(reviewCtx, request)
		if reviewErr == nil {
			reviewErr = reviewCtx.Err()
		}
		cancel()
		if _, ok := errors.AsType[*reviewProgressError](reviewErr); ok {
			return nil, reviewErr
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if reviewErr != nil {
			assessment = security.RiskAssessment{
				Verdict: security.RiskUncertain,
				Reason:  "Risk assessment failed or returned an invalid result; manual approval is required.",
			}
		}
		attempt := request.Attempt
		attempt.Assessment, attempt.CompletedAt = assessment, e.deps.Clock.Now().UTC()
		record := ReviewRecord{
			Key:        call.Key,
			SessionID:  call.SessionID,
			Arguments:  args,
			Decision:   request.Decision,
			RiskReview: attempt,
		}
		e.mu.Lock()
		err = e.checkUnfinished(ctx, call)
		if err == nil {
			e.active[call.Key].review = &record
		}
		e.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
}

func (e *Executor) notifyWaiting(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage, operationID string, sourceTaskID identity.TaskID) {
	call.Arguments = args
	value := toolHookEvent(call, args, operationID)
	value.SourceTaskID = sourceTaskID
	if err := e.deps.HookDispatcher.ToolExecutionWaiting(context.WithoutCancel(ctx), value); err != nil && e.deps.Logf != nil {
		e.deps.Logf("tool execution waiting hook failed: %v", err)
	}
	e.deps.Emitter.Emit(coreevent.Event{
		Scope: coreevent.Scope{
			SessionID:   call.SessionID,
			AgentID:     call.AgentID,
			ExecutionID: call.ExecutionID,
			TurnID:      call.TurnID,
		},
		Payload: coreevent.ToolApprovalRequired{
			CallID: call.CallID,
			Name:   string(call.Name),
			Input:  bytes.Clone(args),
		},
	})
}

// evaluate 只读取事实并作出瞬时结论，不保存工具执行状态或发起模型请求。
func (e *Executor) evaluate(callState *callState, decision security.ApprovalDecision) (authorizationResult, error) {
	deny := func(code, message string) authorizationResult {
		result := tool.NewToolError(code, message)
		return authorizationResult{denied: &result}
	}
	if decision.Action == security.ApprovalDeny {
		return deny("tool_not_allowed", "The operation is prohibited by rule: "+decision.RuleID), nil
	}
	if callState.decision != nil {
		if !callState.decision.Approved {
			return deny("tool_approval_denied", "The user denied this tool call."), nil
		}
		return authorizationResult{}, nil
	}
	switch decision.Action {
	case security.ApprovalYolo:
		return authorizationResult{}, nil
	case security.ApprovalAsk:
		return authorizationResult{pending: true}, nil
	case security.ApprovalAuto:
		review := callState.review
		if review == nil {
			return authorizationResult{review: true}, nil
		}
		switch review.Assessment.Verdict {
		case security.RiskSafe:
			return authorizationResult{}, nil
		case security.RiskDangerous:
			return deny("tool_risk_denied", "Risk review denied this operation: "+review.Assessment.Reason), nil
		default:
			return authorizationResult{pending: true}, nil
		}
	default:
		return authorizationResult{}, errors.New("tool approval decision is invalid")
	}
}

func (e *Executor) checkUnfinished(ctx context.Context, call sessionentry.ToolCall) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	callState := e.active[call.Key]
	if callState == nil {
		return errors.New("tool call is no longer active")
	}
	if _, found := callState.run.Result(call.Key); found {
		return errResultAvailable
	}
	if callState.stopped {
		return errCallStopped
	}
	return nil
}
