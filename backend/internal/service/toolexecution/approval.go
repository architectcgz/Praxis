package toolexecution

import (
	"context"
	"encoding/json"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

// Approval 是消息声明与可靠审批事实组合的查询结果。
type Approval struct {
	Call      sessionentry.ToolCall
	Arguments json.RawMessage
	Decision  security.ApprovalDecision
	Review    executionmodel.RiskReview
}

func (s *Service) ListApprovals(ctx context.Context, agentID identity.AgentID) ([]Approval, error) {
	result := make([]Approval, 0)
	missing, err := s.deps.Calls.ListMissingToolResults(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range missing {
		if item.AgentID != agentID {
			continue
		}
		execution, err := s.deps.Tasks.Get(ctx, item.ExecutionID)
		if err != nil {
			return nil, err
		}
		if !execution.Active() {
			continue
		}
		key := executionmodel.Key{ExecutionID: item.ExecutionID, CallID: item.CallID}
		request, found, err := s.deps.Events.GetApprovalRequest(ctx, key)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		_, decided, err := s.deps.Events.GetUserDecision(ctx, key)
		if err != nil {
			return nil, err
		}
		if decided {
			continue
		}
		call, err := s.deps.Calls.GetCall(ctx, key)
		if err != nil {
			return nil, err
		}
		review, _, err := s.deps.Events.GetReview(ctx, key)
		if err != nil {
			return nil, err
		}
		if _, found, err := s.GetResult(ctx, key); err != nil {
			return nil, err
		} else if found {
			continue
		}
		result = append(result, Approval{
			Call:      call,
			Arguments: request.Arguments,
			Decision:  request.Decision,
			Review:    review.RiskReview,
		})
	}
	return result, nil
}
