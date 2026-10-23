package toolexecution

import (
	"encoding/json"
	"praxis/internal/core/security"
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
