package toolexecution

import (
	"praxis/internal/core/security"
	"praxis/internal/identity"
	"time"
)

type ApprovalSource string

const (
	ApprovalSourceUser ApprovalSource = "user"
	ApprovalSourceYolo ApprovalSource = "yolo"
	ApprovalSourceAuto ApprovalSource = "auto"
)

type ApprovalRecord struct {
	Source     ApprovalSource
	ApprovedAt time.Time
}

type RiskReview struct {
	TurnID      identity.TurnID
	ProviderID  string
	ModelID     string
	StartedAt   time.Time
	CompletedAt time.Time
	Assessment  security.RiskAssessment
}

// ApprovalFacts 只表示审批和审核事实，不包含执行控制状态。
type ApprovalFacts struct {
	RequiredAt time.Time
	Decision   security.ApprovalDecision
	Summary    string
	Approval   ApprovalRecord
	Review     RiskReview
}

func (f ApprovalFacts) AwaitingApproval() bool {
	return !f.RequiredAt.IsZero() && f.Approval.ApprovedAt.IsZero()
}

// Allows 判断已有批准是否适用于当前规则，执行状态由调用方检查。
func (f ApprovalFacts) Allows(decision security.ApprovalDecision) bool {
	return !f.Approval.ApprovedAt.IsZero() && (f.Decision == decision || decision.Action == security.ApprovalAsk && f.Approval.Source == ApprovalSourceUser)
}
