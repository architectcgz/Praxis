package toolexecution

import (
	"context"
	"encoding/json"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"time"
)

// ApprovalRequest 保存一次人工审批所针对的冻结参数和生效规则。
type ApprovalRequest struct {
	Key
	SessionID   identity.SessionID
	Arguments   json.RawMessage
	Decision    security.ApprovalDecision
	RequestedAt time.Time
}

// UserDecision 绑定当前 ToolCall 唯一的审批请求。
type UserDecision struct {
	Key
	SessionID identity.SessionID
	Approved  bool
	DecidedAt time.Time
}

type RiskReview struct {
	TurnID      identity.TurnID
	ProviderID  string
	ModelID     string
	StartedAt   time.Time
	CompletedAt time.Time
	Assessment  security.RiskAssessment
}

// ReviewRecord 将模型审核证据绑定到冻结操作与当时的规则。
type ReviewRecord struct {
	Key
	SessionID identity.SessionID
	Arguments json.RawMessage
	Decision  security.ApprovalDecision
	RiskReview
}

// Approval 是当前活跃调用的审批信息，参数与内部状态共享底层数据，调用方只读；工具结束后不再提供查询。
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
	for _, callState := range e.active {
		if callState.call.AgentID != agentID || callState.stopped || callState.request == nil || callState.decision != nil || callState.tool != nil {
			continue
		}
		value := Approval{
			Call:      callState.call,
			Arguments: callState.request.Arguments,
			Decision:  callState.request.Decision,
		}
		if callState.review != nil {
			value.Review = callState.review.RiskReview
		}
		result = append(result, value)
	}
	return result, nil
}
