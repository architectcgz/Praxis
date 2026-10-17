package toolinvocation

import (
	"time"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
)

// validateAuthorization 校验授权与审核记录的不变量；不访问模型、仓储或当前配置。
func (i ToolInvocation) validateAuthorization() error {
	if (i.Status == ToolInvocationPrepared || i.Status == ToolInvocationReviewing) && len(i.NormalizedArguments) == 0 {
		return contracts.InvalidValue("toolInvocation.arguments", "prepared operation requires frozen arguments")
	}
	if i.Decision != (security.ApprovalDecision{}) && (!i.Decision.Action.Valid() || i.Decision.RuleID == "") {
		return contracts.InvalidValue("toolInvocation.decision", "a valid action and rule identity are required")
	}
	for index, review := range i.Reviews {
		if review.TurnID == "" || review.ArgumentsDigest != ArgumentsDigest(i.Name, i.NormalizedArguments) || review.ProviderID == "" || review.ModelID == "" || review.StartedAt.Before(i.CreatedAt) || review.StartedAt.IsZero() {
			return contracts.InvalidValue("toolInvocation.review", "risk review must match the frozen operation and model identity")
		}
		if review.CompletedAt.IsZero() {
			if index != len(i.Reviews)-1 || i.Status != ToolInvocationReviewing && i.Status != ToolInvocationInterrupted || review.Assessment != (security.RiskAssessment{}) {
				return contracts.InvalidValue("toolInvocation.review", "only an active or interrupted review may be incomplete")
			}
		} else if review.CompletedAt.Before(review.StartedAt) {
			return contracts.InvalidValue("toolInvocation.review", "risk review completion precedes its start")
		} else if err := review.Assessment.Validate(); err != nil {
			return err
		}
	}
	if i.Status == ToolInvocationReviewing && (len(i.Reviews) == 0 || !i.LatestReview().CompletedAt.IsZero() || i.Decision.Action != security.ApprovalAuto) {
		return contracts.InvalidValue("toolInvocation.review", "reviewing operation requires a pending auto assessment")
	}
	if i.Approval.Source == contracts.ApprovalSourceAuto && (i.LatestReview().Assessment.Verdict != security.RiskSafe || i.Decision.Action != security.ApprovalAuto) {
		return contracts.InvalidValue("toolInvocation.approval", "auto approval requires an auto decision and durable safe assessment")
	}
	if i.Decision != (security.ApprovalDecision{}) {
		if i.Approval.Source == contracts.ApprovalSourceYolo && i.Decision.Action != security.ApprovalYolo {
			return contracts.InvalidValue("toolInvocation.approval", "yolo approval cannot satisfy a different action")
		}
		if i.Approval.Source == contracts.ApprovalSourceUser && i.Decision.Action == security.ApprovalAuto && i.LatestReview().Assessment.Verdict != security.RiskUncertain {
			return contracts.InvalidValue("toolInvocation.approval", "manual approval of an auto operation requires an uncertain assessment")
		}
	}
	return nil
}

// ApplyAuthorization 根据本次规则决定等待人工、等待模型、yolo 批准或拒绝。
// 调用层只提供已解析规则和时间，不直接设置领域状态或审批字段。
func (i *ToolInvocation) ApplyAuthorization(decision security.ApprovalDecision, at time.Time) error {
	if i.Status != ToolInvocationPrepared || !decision.Action.Valid() || decision.RuleID == "" {
		return contracts.ErrRequestConflict
	}
	next := *i
	next.Decision = decision
	switch decision.Action {
	case security.ApprovalAsk:
		next.Status = ToolInvocationAwaitingApproval
	case security.ApprovalAuto:
		// 参数已冻结，必须取得独立模型审核结论后才可自动批准。
	case security.ApprovalYolo:
		approval, err := contracts.NewYoloApproval(ArgumentsDigest(i.Name, i.NormalizedArguments), at)
		if err != nil {
			return err
		}
		next.Status = ToolInvocationApproved
		next.Approval = approval
		next.ApprovedAt = approval.ApprovedAt
	case security.ApprovalDeny:
		next.Status = ToolInvocationDenied
		next.FailureCode = ToolFailureNotAllowed
		next.Result = ToolInvocationResult{InlineContent: "The operation is prohibited by rule: " + decision.RuleID, ErrorCode: ToolFailureNotAllowed}
		next.SettledAt = at.UTC()
	}
	return i.apply(next)
}

// NeedsRiskReview 表示该操作尚缺模型结论；人工等待不能被模式切换静默代批。
func (i ToolInvocation) NeedsRiskReview() bool {
	return i.Status == ToolInvocationReviewing || i.Status == ToolInvocationPrepared && i.Decision.Action == security.ApprovalAuto
}

// RecheckAuthorization 在启动前处理硬性禁止或规则变化，已启动调用不参与重新授权。
func (i *ToolInvocation) RecheckAuthorization(decision security.ApprovalDecision, at time.Time) error {
	if !decision.Action.Valid() || decision.RuleID == "" {
		return contracts.ErrRequestConflict
	}
	if i.Status != ToolInvocationApproved && i.Status != ToolInvocationAwaitingApproval {
		return nil
	}
	if decision.Action == security.ApprovalDeny {
		message := "The current configuration prohibits this operation. Rule: " + decision.RuleID
		if i.Status == ToolInvocationAwaitingApproval {
			return i.Deny(ToolInvocationResult{InlineContent: message, ErrorCode: ToolFailureNotAllowed}, at)
		}
		return i.Interrupt(ToolInvocationResult{InlineContent: message, ErrorCode: ToolFailureInterrupted}, at)
	}
	if i.Status == ToolInvocationApproved && i.Decision != decision {
		// 用户已明确批准同一次冻结操作，仍满足当前 ask 要求，无须重复询问。
		if decision.Action == security.ApprovalAsk && i.Approval.Source == contracts.ApprovalSourceUser {
			return nil
		}
		return i.RevokeApproval()
	}
	return nil
}
