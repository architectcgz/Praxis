package toolcall

import (
	"praxis/internal/fault"
	"praxis/internal/security"
	"time"
)

// validateAuthorization 检查当前授权与最近审核的关系，不回扫已冻结的历史文本和身份格式。
func (i Execution) validateAuthorization() error {
	if (i.Status == StatusPrepared || i.Status == StatusReviewing) && len(i.NormalizedArguments) == 0 {
		return fault.InvalidValue("toolExecution.arguments", "prepared operation requires frozen arguments")
	}
	if i.Decision != (security.ApprovalDecision{}) && (!i.Decision.Action.Valid() || i.Decision.RuleID == "") {
		return fault.InvalidValue("toolExecution.decision", "a valid action and rule identity are required")
	}
	if review := i.LatestReview(); len(i.Reviews) > 0 {
		if review.ArgumentsDigest != ArgumentsDigest(i.Name, i.NormalizedArguments) || review.StartedAt.Before(i.CreatedAt) || review.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution.review", "risk review must match the frozen operation and model identity")
		}
		if review.CompletedAt.IsZero() {
			if i.Status != StatusReviewing && i.Status != StatusInterrupted || review.Assessment != (security.RiskAssessment{}) {
				return fault.InvalidValue("toolExecution.review", "only an active or interrupted review may be incomplete")
			}
		} else if review.CompletedAt.Before(review.StartedAt) {
			return fault.InvalidValue("toolExecution.review", "risk review completion precedes its start")
		}
	}
	if i.Status == StatusReviewing && (len(i.Reviews) == 0 || !i.LatestReview().CompletedAt.IsZero() || i.Decision.Action != security.ApprovalAuto) {
		return fault.InvalidValue("toolExecution.review", "reviewing operation requires a pending auto assessment")
	}
	if i.Approval.Source == ApprovalSourceAuto && (i.LatestReview().Assessment.Verdict != security.RiskSafe || i.Decision.Action != security.ApprovalAuto) {
		return fault.InvalidValue("toolExecution.approval", "auto approval requires an auto decision and durable safe assessment")
	}
	if i.Decision != (security.ApprovalDecision{}) {
		if i.Approval.Source == ApprovalSourceYolo && i.Decision.Action != security.ApprovalYolo {
			return fault.InvalidValue("toolExecution.approval", "yolo approval cannot satisfy a different action")
		}
		if i.Approval.Source == ApprovalSourceUser && i.Decision.Action == security.ApprovalAuto && i.LatestReview().Assessment.Verdict != security.RiskUncertain {
			return fault.InvalidValue("toolExecution.approval", "manual approval of an auto operation requires an uncertain assessment")
		}
	}
	return nil
}

// ApplyAuthorization 根据本次规则决定等待人工、等待模型、yolo 批准或拒绝。
// 调用层只提供已解析规则和时间，不直接设置领域状态或审批字段。
func (i *Execution) ApplyAuthorization(decision security.ApprovalDecision, at time.Time) error {
	if i.Status != StatusPrepared || !decision.Action.Valid() || decision.RuleID == "" {
		return fault.ErrRequestConflict
	}
	next := *i
	next.Decision = decision
	switch decision.Action {
	case security.ApprovalAsk:
		next.Status = StatusAwaitingApproval
	case security.ApprovalAuto:
		// 参数已冻结，必须取得独立模型审核结论后才可自动批准。
	case security.ApprovalYolo:
		approval := ApprovalRecord{
			Source:          ApprovalSourceYolo,
			ArgumentsDigest: ArgumentsDigest(i.Name, i.NormalizedArguments),
			ApprovedAt:      at.UTC(),
		}
		next.Status = StatusApproved
		next.Approval = approval
		next.ApprovedAt = approval.ApprovedAt
	case security.ApprovalDeny:
		next.Status = StatusDenied
		next.FailureCode = FailureNotAllowed
		next.Result = Result{InlineContent: "The operation is prohibited by rule: " + decision.RuleID, ErrorCode: FailureNotAllowed}
		next.SettledAt = at.UTC()
	}
	return i.apply(next)
}

// NeedsRiskReview 表示该操作尚缺模型结论；人工等待不能被模式切换静默代批。
func (i Execution) NeedsRiskReview() bool {
	return i.Status == StatusReviewing || i.Status == StatusPrepared && i.Decision.Action == security.ApprovalAuto
}

// RecheckAuthorization 在启动前处理硬性禁止或规则变化，已启动调用不参与重新授权。
func (i *Execution) RecheckAuthorization(decision security.ApprovalDecision, at time.Time) error {
	if !decision.Action.Valid() || decision.RuleID == "" {
		return fault.ErrRequestConflict
	}
	if i.Status != StatusApproved && i.Status != StatusAwaitingApproval {
		return nil
	}
	if decision.Action == security.ApprovalDeny {
		message := "The current configuration prohibits this operation. Rule: " + decision.RuleID
		if i.Status == StatusAwaitingApproval {
			return i.Deny(Result{InlineContent: message, ErrorCode: FailureNotAllowed}, at)
		}
		return i.Interrupt(Result{InlineContent: message, ErrorCode: FailureInterrupted}, at)
	}
	if i.Status == StatusApproved && i.Decision != decision {
		// 用户已明确批准同一次冻结操作，仍满足当前 ask 要求，无须重复询问。
		if decision.Action == security.ApprovalAsk && i.Approval.Source == ApprovalSourceUser {
			return nil
		}
		return i.RevokeApproval()
	}
	return nil
}
