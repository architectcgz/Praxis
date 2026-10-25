package toolexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// Approval 是当前活跃调用的独立审批快照，工具结束后不再提供查询。
type Approval struct {
	Call      sessionentry.ToolCall
	Arguments json.RawMessage
	Decision  security.ApprovalDecision
	Review    RiskReview
}

func (e *Executor) ListApprovals(ctx context.Context, agentID identity.AgentID) ([]Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]Approval, 0)
	for _, active := range e.active {
		if active.call.AgentID != agentID || active.stopped || active.request == nil || active.decision != nil || active.tool != nil {
			continue
		}
		call := active.call
		call.Arguments = bytes.Clone(call.Arguments)
		value := Approval{
			Call:      call,
			Arguments: bytes.Clone(active.request.Arguments),
			Decision:  active.request.Decision,
		}
		if active.review != nil {
			value.Review = active.review.RiskReview
		}
		result = append(result, value)
	}
	return result, nil
}
