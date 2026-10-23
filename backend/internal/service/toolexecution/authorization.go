package toolexecution

import (
	"context"
	"encoding/json"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"time"
)

// authorize 根据当前事实确定授权；在所属执行流程内保存审核请求序号，再调用外部模型。
func (scope *Scope) authorize(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (bool, *tool.ToolResult, error) {
	s := scope.service
	digest := executionmodel.ArgumentsDigest(call.Name, args)
	for {
		var pending bool
		var denied *tool.ToolResult
		var task agentexecution.Execution
		var decision security.ApprovalDecision
		var attempt executionmodel.RiskReview
		err := func(txCtx context.Context) error {
			var err error
			task, err = s.deps.Tasks.Get(txCtx, call.ExecutionID)
			if err != nil {
				return err
			}
			if !task.Active() || task.AgentID != call.AgentID || task.SessionID != call.SessionID {
				return fault.ErrRequestConflict
			}
			facts, err := s.deps.Events.Read(txCtx, call.Key)
			if err != nil {
				return err
			}
			if !facts.StopRequestedAt.IsZero() {
				value := tool.NewToolError("tool_interrupted", "The tool call was canceled before execution.")
				denied = &value
				return nil
			}
			decision, err = s.deps.Rules.Decide(txCtx, call, args)
			if err != nil {
				return err
			}
			if decision.Action == security.ApprovalDeny {
				value := tool.NewToolError("tool_not_allowed", "The operation is prohibited by rule: "+decision.RuleID)
				denied = &value
				return nil
			}
			if facts.Approval.ArgumentsDigest == digest && !facts.Approval.ApprovedAt.IsZero() && (facts.Decision == decision || decision.Action == security.ApprovalAsk && facts.Approval.Source == executionmodel.ApprovalSourceUser) {
				return nil
			}
			event := executionmodel.Event{
				Key:             call.Key,
				SessionID:       call.SessionID,
				At:              s.clock.Now().UTC(),
				ArgumentsDigest: digest,
				Decision:        decision,
				Summary:         string(args),
			}
			if facts.AwaitingApproval() && facts.ArgumentsDigest == digest && facts.Decision == decision {
				pending = true
				return nil
			}
			if decision.Action == security.ApprovalAsk || facts.AwaitingApproval() {
				event.Kind = executionmodel.ApprovalRequired
				pending = true
				return s.deps.Events.Append(txCtx, event)
			}
			if decision.Action == security.ApprovalYolo {
				event.Kind, event.Source = executionmodel.Approved, executionmodel.ApprovalSourceYolo
				return s.deps.Events.Append(txCtx, event)
			}
			if facts.Review.ArgumentsDigest == digest {
				switch facts.Review.Assessment.Verdict {
				case security.RiskDangerous:
					value := tool.NewToolError("tool_risk_denied", "Risk review denied this operation: "+facts.Review.Assessment.Reason)
					denied = &value
					return nil
				case security.RiskSafe:
					event.Kind, event.Source = executionmodel.Approved, executionmodel.ApprovalSourceAuto
				default:
					event.Kind, pending = executionmodel.ApprovalRequired, true
				}
				return s.deps.Events.Append(txCtx, event)
			}
			sequence := max(uint64(1), task.Progress.NextSequence)
			task.Progress.NextSequence, task.Progress.ModelRequests = sequence+1, task.Progress.ModelRequests+1
			attempt = executionmodel.RiskReview{
				TurnID:          identity.NewTurnID(task.ID, sequence),
				ArgumentsDigest: digest,
				ProviderID:      task.Config.Model.ProviderID,
				ModelID:         task.Config.Model.ModelID,
				StartedAt:       s.clock.Now().UTC(),
			}
			return s.deps.Tasks.Save(txCtx, task)
		}(ctx)
		if err != nil || pending || denied != nil || attempt.TurnID == "" {
			return pending, denied, err
		}
		reviewCtx, cancel := context.WithTimeout(ctx, time.Minute)
		assessment, err := s.deps.Review(reviewCtx, task, call, args, scope.workspacePath, decision, attempt)
		if err == nil {
			err = reviewCtx.Err()
		}
		cancel()
		if ctx.Err() != nil {
			return false, nil, ctx.Err()
		}
		if err != nil {
			assessment = security.RiskAssessment{Verdict: security.RiskUncertain, Reason: "Risk assessment failed or returned an invalid result; manual approval is required."}
		}
		attempt.Assessment, attempt.CompletedAt = assessment, s.clock.Now().UTC()
		err = func(txCtx context.Context) error {
			facts, err := s.deps.Events.Read(txCtx, call.Key)
			if err != nil {
				return err
			}
			if !facts.StopRequestedAt.IsZero() {
				return nil
			}
			return s.deps.Events.Append(txCtx, executionmodel.Event{
				Key:       call.Key,
				SessionID: call.SessionID,
				Kind:      executionmodel.Reviewed,
				At:        attempt.CompletedAt,
				Review:    &attempt,
			})
		}(ctx)
		if err != nil {
			return false, nil, err
		}
	}
}
