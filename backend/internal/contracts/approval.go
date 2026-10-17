package contracts

import (
	"time"
)

type ApprovalSource string

const (
	ApprovalSourceUser ApprovalSource = "user"
	ApprovalSourceYolo ApprovalSource = "yolo"
	ApprovalSourceAuto ApprovalSource = "auto"
)

func (s ApprovalSource) Valid() bool {
	return s == ApprovalSourceUser || s == ApprovalSourceYolo || s == ApprovalSourceAuto
}

type ApprovalRecord struct {
	Source          ApprovalSource
	ArgumentsDigest string
	ApprovedAt      time.Time
}

func (a ApprovalRecord) Validate() error {
	if !a.Source.Valid() {
		return InvalidValue("approval.source", "unknown approval source")
	}
	if a.ApprovedAt.IsZero() {
		return InvalidValue("approval.approvedAt", "approval time is required")
	}
	if EmptyID(a.ArgumentsDigest) {
		return InvalidValue("approval.argumentsDigest", "approval requires an arguments digest")
	}
	return nil
}

// NewYoloApproval 为本次冻结参数创建跳过风险审核的批准，不覆盖硬性禁止规则。
func NewYoloApproval(digest string, at time.Time) (ApprovalRecord, error) {
	if EmptyID(digest) {
		return ApprovalRecord{}, InvalidValue("approval.argumentsDigest", "arguments digest is required")
	}
	return ApprovalRecord{
		Source:          ApprovalSourceYolo,
		ArgumentsDigest: digest,
		ApprovedAt:      at.UTC(),
	}, nil
}
