package toolexecution

import (
	"bytes"
	"praxis/internal/core/security"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"slices"
	"time"
)

// RiskReview 绑定一次独立模型审核及冻结参数；未完成的审核不是工具已启动的证据。
type RiskReview struct {
	TurnID          identity.TurnID
	ArgumentsDigest string
	ProviderID      string
	ModelID         string
	StartedAt       time.Time
	CompletedAt     time.Time
	Assessment      security.RiskAssessment
}

// LatestReview 返回最近一次审核的值副本，没有审核时返回零值。
func (i Execution) LatestReview() RiskReview {
	if len(i.Reviews) == 0 {
		return RiskReview{}
	}
	return i.Reviews[len(i.Reviews)-1]
}

// Prepare 一次冻结执行参数，后续审批、审核及重启均不得重新规范化。
func (i *Execution) Prepare(arguments []byte, path string) error {
	next := *i
	next.Status = StatusPrepared
	next.NormalizedArguments = bytes.Clone(arguments)
	next.Path = path
	return i.apply(next)
}

// BeginReview 保存审核身份；重启遗留的未完成审核收敛为不确定，再开始新请求。
func (i *Execution) BeginReview(review RiskReview) error {
	next := *i
	next.Reviews = slices.Clone(i.Reviews)
	if previous := i.LatestReview(); previous.TurnID != "" && previous.CompletedAt.IsZero() {
		previous.CompletedAt = review.StartedAt
		previous.Assessment = security.RiskAssessment{Verdict: security.RiskUncertain, Reason: "The previous risk review was interrupted before its result was saved."}
		next.Reviews[len(next.Reviews)-1] = previous
	}
	next.Reviews = append(next.Reviews, review)
	next.Status = StatusReviewing
	return i.apply(next)
}

// ApplyRiskAssessment 将模型结论转换为领域状态，只有当前审核身份可提交结果。
// 危险直接拒绝，不确定进入人工等待；安全批准前还要确认当前规则没有变化。
func (i *Execution) ApplyRiskAssessment(turnID identity.TurnID, assessment security.RiskAssessment, current security.ApprovalDecision, at time.Time) error {
	if i.Status != StatusReviewing || i.LatestReview().TurnID != turnID || !current.Action.Valid() || current.RuleID == "" {
		return fault.ErrRequestConflict
	}
	next := *i
	next.Reviews = slices.Clone(i.Reviews)
	last := len(next.Reviews) - 1
	next.Reviews[last].Assessment = assessment
	next.Reviews[last].CompletedAt = at.UTC()
	switch {
	case current.Action == security.ApprovalDeny:
		next.Status = StatusDenied
		next.FailureCode = FailureNotAllowed
		next.Result = Result{InlineContent: "The operation is prohibited by rule: " + current.RuleID, ErrorCode: FailureNotAllowed}
		next.SettledAt = at.UTC()
	case assessment.Verdict == security.RiskDangerous:
		next.Status = StatusDenied
		next.FailureCode = FailureRiskDenied
		next.Result = Result{InlineContent: "Risk review denied this operation: " + assessment.Reason, ErrorCode: FailureRiskDenied}
		next.SettledAt = at.UTC()
	case assessment.Verdict == security.RiskUncertain:
		next.Status = StatusAwaitingApproval
	case current != i.Decision:
		// 审核期间规则已变化：保留结论，但不能把旧规则的批准带入新动作。
		next.Status = StatusPrepared
	case assessment.Verdict == security.RiskSafe:
		next.Status = StatusApproved
		next.Approval = ApprovalRecord{Source: ApprovalSourceAuto, ArgumentsDigest: next.LatestReview().ArgumentsDigest, ApprovedAt: at.UTC()}
		next.ApprovedAt = at.UTC()
	}
	return i.apply(next)
}

// ResetReview 在规则变化时关闭尚未完成的旧审核，再按新规则授权，不能复用旧模型结论。
func (i *Execution) ResetReview(decision security.ApprovalDecision, at time.Time) error {
	if i.Status != StatusReviewing {
		return fault.ErrRequestConflict
	}
	next := *i
	next.Reviews = slices.Clone(i.Reviews)
	last := len(next.Reviews) - 1
	next.Reviews[last].CompletedAt = at.UTC()
	next.Reviews[last].Assessment = security.RiskAssessment{Verdict: security.RiskUncertain, Reason: "The authorization rule changed before the risk review completed."}
	next.Status = StatusPrepared
	if err := next.ApplyAuthorization(decision, at); err != nil {
		return err
	}
	return i.apply(next)
}

// RevokeApproval 撤销尚未启动的旧授权，保留所有冻结参数及审核记录。
func (i *Execution) RevokeApproval() error {
	if i.Status != StatusApproved {
		return fault.ErrRequestConflict
	}
	next := *i
	next.Status = StatusPrepared
	next.Approval = ApprovalRecord{}
	next.ApprovedAt = time.Time{}
	return i.apply(next)
}
