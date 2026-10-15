package contracts

import (
	"time"
)

type ApprovalSource string

const (
	ApprovalSourceUser          ApprovalSource = "user"
	ApprovalSourcePolicyDefault ApprovalSource = "policy_default"
)

func (s ApprovalSource) Valid() bool {
	return s == ApprovalSourceUser || s == ApprovalSourcePolicyDefault
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

func NewPolicyApproval(digest string, at time.Time) (ApprovalRecord, error) {
	if EmptyID(digest) {
		return ApprovalRecord{}, InvalidValue("approval.argumentsDigest", "arguments digest is required")
	}
	return ApprovalRecord{
		Source:          ApprovalSourcePolicyDefault,
		ArgumentsDigest: digest,
		ApprovedAt:      at.UTC(),
	}, nil
}
