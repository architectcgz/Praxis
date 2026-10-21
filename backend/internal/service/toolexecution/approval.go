package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/message"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// ApprovalRequest 绑定审批请求的会话、Agent 和具体调用。
type ApprovalRequest struct {
	SessionID       identity.SessionID
	AgentID         identity.AgentID
	ExecutionID     identity.AgentExecutionID
	CallID          string
	ArgumentsDigest string
}

// Approval 是消息声明和审批事件组合的查询结果，不是持久化实体。
type Approval struct {
	Call            message.ToolCall
	ArgumentsDigest string
	Arguments       json.RawMessage
	Decision        security.ApprovalDecision
	Review          executionmodel.RiskReview
}

func (s *Service) Approve(ctx context.Context, executionID identity.AgentExecutionID, callID, digest string) (agentmodel.Execution, error) {
	return s.Decide(ctx, ApprovalRequest{ExecutionID: executionID, CallID: callID, ArgumentsDigest: digest}, true)
}

func (s *Service) Deny(ctx context.Context, executionID identity.AgentExecutionID, callID, digest string) (agentmodel.Execution, error) {
	return s.Decide(ctx, ApprovalRequest{ExecutionID: executionID, CallID: callID, ArgumentsDigest: digest}, false)
}

func (s *Service) Decide(ctx context.Context, request ApprovalRequest, approve bool) (task agentmodel.Execution, err error) {
	executionID, callID, digest := request.ExecutionID, request.CallID, request.ArgumentsDigest
	err = s.deps.Transactions.InTx(ctx, func(txCtx context.Context) error {
		call, err := s.call(txCtx, executionID, callID)
		if err != nil {
			return err
		}
		if request.SessionID != "" && request.SessionID != call.SessionID || request.AgentID != "" && request.AgentID != call.AgentID {
			return fault.ErrRequestConflict
		}
		task, err = s.deps.Tasks.Get(txCtx, executionID)
		if err != nil {
			return err
		}
		facts, err := s.deps.Events.Read(txCtx, call.Key)
		if err != nil {
			return err
		}
		if digest != facts.ArgumentsDigest {
			return fault.ErrRequestConflict
		}
		if approve && facts.Approval.Source == executionmodel.ApprovalSourceUser && facts.Approval.ArgumentsDigest == digest {
			return nil
		}
		if result, err := s.deps.Calls.GetResult(txCtx, call.Key); err == nil {
			if !approve && len(result.Blocks) == 1 && result.Blocks[0].Text == tool.NewToolError("tool_approval_denied", "The user denied this tool call.").Payload {
				return nil
			}
			return fault.ErrRequestConflict
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		if !task.Active() || !facts.AwaitingApproval() {
			return fault.ErrRequestConflict
		}
		if !approve {
			_, _, err := s.appendError(txCtx, call, "tool_approval_denied", "The user denied this tool call.")
			return err
		}
		decision, err := s.deps.Rules.Decide(txCtx, call, json.RawMessage(facts.Summary))
		if err != nil {
			return err
		}
		if decision.Action == security.ApprovalDeny {
			_, _, err := s.appendError(txCtx, call, "tool_not_allowed", "The current configuration prohibits this operation. Rule: "+decision.RuleID)
			return err
		}
		// 只批准展示的那份操作；启动前仍按最新规则重新授权或审核。
		return s.deps.Events.Append(txCtx, executionmodel.Event{
			Key:             call.Key,
			SessionID:       call.SessionID,
			Kind:            executionmodel.Approved,
			At:              s.clock.Now().UTC(),
			ArgumentsDigest: digest,
			Decision:        facts.Decision,
			Source:          executionmodel.ApprovalSourceUser,
			Summary:         facts.Summary,
		})
	})
	return task, err
}

func (s *Service) ListApprovals(ctx context.Context, agentID identity.AgentID) ([]Approval, error) {
	result := make([]Approval, 0)
	err := s.deps.Transactions.InTx(ctx, func(txCtx context.Context) error {
		missing, err := s.deps.Calls.ListMissingToolResults(txCtx)
		if err != nil {
			return err
		}
		for _, item := range missing {
			if item.AgentID != agentID {
				continue
			}
			task, err := s.deps.Tasks.Get(txCtx, item.ExecutionID)
			if err != nil {
				return err
			}
			if !task.Active() {
				continue
			}
			key := executionmodel.Key{ExecutionID: item.ExecutionID, CallID: item.CallID}
			facts, err := s.deps.Events.Read(txCtx, key)
			if err != nil {
				return err
			}
			if !facts.AwaitingApproval() {
				continue
			}
			call, err := s.deps.Calls.GetCall(txCtx, key)
			if err != nil {
				return err
			}
			result = append(result, Approval{
				Call:            call,
				ArgumentsDigest: facts.ArgumentsDigest,
				Arguments:       json.RawMessage(facts.Summary),
				Decision:        facts.Decision,
				Review:          facts.Review,
			})
		}
		return nil
	})
	return result, err
}
