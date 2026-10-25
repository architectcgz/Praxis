package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"time"
)

var errResultAvailable = errors.New("tool result is already committed")
var errCallStopped = errors.New("tool call was stopped")

type authorizationResult struct {
	pending bool
	denied  *tool.ToolResult
	review  bool
}

// authorize 沿用本次 ToolCall 的规则决定；唤醒后只读取审批事实，模型审核在控制锁外执行。
// args 由执行流程冻结，审批请求、审核输入和审核记录共享只读参数。
func (t *ExecutionTools) authorize(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage, decision security.ApprovalDecision) (bool, *tool.ToolResult, error) {
	e := t.executor
	for {
		result, request, err := t.authorizationStep(ctx, call, args, decision)
		if err != nil {
			return false, nil, err
		}
		if request == nil {
			return result.pending, result.denied, nil
		}
		reviewCtx, cancel := context.WithTimeout(ctx, time.Minute)
		assessment, reviewErr := e.deps.Reviewer.review(reviewCtx, *request)
		if reviewErr == nil {
			reviewErr = reviewCtx.Err()
		}
		cancel()
		if _, ok := errors.AsType[*reviewProgressError](reviewErr); ok {
			return false, nil, reviewErr
		}
		if ctx.Err() != nil {
			return false, nil, ctx.Err()
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
			return false, nil, err
		}
	}
}

func (t *ExecutionTools) authorizationStep(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage, decision security.ApprovalDecision) (authorizationResult, *ReviewRequest, error) {
	e := t.executor
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.checkUnfinished(ctx, call); err != nil {
		return authorizationResult{}, nil, err
	}
	callState := e.active[call.Key]
	result, err := e.evaluate(callState, decision)
	if err != nil {
		return result, nil, err
	}
	if result.pending {
		if callState.request == nil {
			callState.request = &ApprovalRequest{
				Key:         call.Key,
				SessionID:   call.SessionID,
				Arguments:   args,
				Decision:    decision,
				RequestedAt: e.deps.Clock.Now().UTC(),
			}
		}
		return result, nil, nil
	}
	if !result.review {
		return result, nil, nil
	}
	request, err := e.deps.Operations.PrepareReview(ctx, t.run, call, t.workspacePath)
	if err != nil {
		return result, nil, err
	}
	request.Call, request.Arguments, request.Decision = call, args, decision
	return result, &request, nil
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
