package authorization

import (
	"context"
	"encoding/json"
	"praxis/internal/clock"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"time"
)

// Authorizer 拥有授权流程、模型审核和人工审批通知，不读取仓储或执行工具。
type Authorizer struct {
	Provider       providerapi.Provider
	TimingRecorder *timing.Recorder
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Clock          clock.Clock
	Logf           func(string, ...any)
}

// ReviewRequest 由调用方准备任务输入并预留请求序号，模型调用由 Core 完成。
type ReviewRequest struct {
	Model         modelapi.Config
	UserTask      string
	WorkspacePath string
	Attempt       executionmodel.RiskReview
}

type Input struct {
	Call      sessionentry.ToolCall
	Arguments json.RawMessage
	Decision  security.ApprovalDecision
	Facts     executionmodel.ApprovalFacts
	Review    *ReviewRequest
}

// Result 给出本次授权结论和待保存的事实；审核事实保存后需重新读取当前规则。
type Result struct {
	Pending        bool
	Denied         *tool.ToolResult
	Event          *executionmodel.Event
	ReviewRequired bool
}

func (a *Authorizer) Authorize(ctx context.Context, input Input) (Result, error) {
	facts, decision := input.Facts, input.Decision
	if decision.Action == security.ApprovalDeny {
		return denied("tool_not_allowed", "The operation is prohibited by rule: "+decision.RuleID), nil
	}
	if facts.Allows(decision) {
		return Result{}, nil
	}
	if facts.AwaitingApproval() && facts.Decision == decision {
		return Result{Pending: true}, nil
	}
	event := a.approvalEvent(input.Call, decision, string(input.Arguments))
	if decision.Action == security.ApprovalAsk || facts.AwaitingApproval() {
		event.Kind = executionmodel.ApprovalRequired
		return Result{Pending: true, Event: &event}, nil
	}
	if decision.Action == security.ApprovalYolo {
		event.Kind, event.Source = executionmodel.Approved, executionmodel.ApprovalSourceYolo
		return Result{Event: &event}, nil
	}
	if facts.Review.TurnID != "" {
		switch facts.Review.Assessment.Verdict {
		case security.RiskDangerous:
			return denied("tool_risk_denied", "Risk review denied this operation: "+facts.Review.Assessment.Reason), nil
		case security.RiskSafe:
			event.Kind, event.Source = executionmodel.Approved, executionmodel.ApprovalSourceAuto
			return Result{Event: &event}, nil
		default:
			event.Kind = executionmodel.ApprovalRequired
			return Result{Pending: true, Event: &event}, nil
		}
	}
	if input.Review == nil {
		return Result{ReviewRequired: true}, nil
	}
	reviewCtx, cancel := context.WithTimeout(ctx, time.Minute)
	assessment, err := a.review(reviewCtx, input)
	if err == nil {
		err = reviewCtx.Err()
	}
	cancel()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err != nil {
		assessment = security.RiskAssessment{
			Verdict: security.RiskUncertain,
			Reason:  "Risk assessment failed or returned an invalid result; manual approval is required.",
		}
	}
	attempt := input.Review.Attempt
	attempt.Assessment, attempt.CompletedAt = assessment, a.now()
	return Result{Event: &executionmodel.Event{
		Key:       input.Call.Key,
		SessionID: input.Call.SessionID,
		Kind:      executionmodel.Reviewed,
		At:        attempt.CompletedAt,
		Review:    &attempt,
	}}, nil
}

// DecideApproval 将人工决定转为批准事实或工具拒绝结果，归属与执行状态由调用方检查。
func (a *Authorizer) DecideApproval(call sessionentry.ToolCall, facts executionmodel.ApprovalFacts, current security.ApprovalDecision, approve bool) Result {
	if !approve {
		return denied("tool_approval_denied", "The user denied this tool call.")
	}
	if current.Action == security.ApprovalDeny {
		return denied("tool_not_allowed", "The current configuration prohibits this operation. Rule: "+current.RuleID)
	}
	event := a.approvalEvent(call, facts.Decision, facts.Summary)
	event.Kind, event.Source = executionmodel.Approved, executionmodel.ApprovalSourceUser
	return Result{Event: &event}
}

// NotifyWaiting 在审批事实与执行等待状态保存成功后调用，通知不负责修改执行状态。
func (a *Authorizer) NotifyWaiting(ctx context.Context, value hook.ToolExecution) {
	if err := a.HookDispatcher.ToolExecutionWaiting(context.WithoutCancel(ctx), value); err != nil && a.Logf != nil {
		a.Logf("tool execution waiting hook failed: %v", err)
	}
	a.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.ToolApprovalRequired,
		SessionID:   value.SessionID,
		AgentID:     value.AgentID,
		ExecutionID: value.ExecutionID,
		TurnID:      value.TurnID,
		CallID:      value.Call.CallID,
		Name:        string(value.Call.Name),
	})
}

func (a *Authorizer) approvalEvent(call sessionentry.ToolCall, decision security.ApprovalDecision, summary string) executionmodel.Event {
	return executionmodel.Event{
		Key:       call.Key,
		SessionID: call.SessionID,
		At:        a.now(),
		Decision:  decision,
		Summary:   summary,
	}
}

func (a *Authorizer) now() time.Time { return clock.ClockOrDefault(a.Clock).Now().UTC() }

func denied(code, message string) Result {
	value := tool.NewToolError(code, message)
	return Result{Denied: &value}
}
