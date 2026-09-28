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

type ApprovalMode string

const (
	ApprovalAlwaysAsk           ApprovalMode = "always_ask"
	ApprovalAutoApproveDefaults ApprovalMode = "auto_approve_defaults"
	ApprovalYolo                ApprovalMode = "yolo"
)

// Valid reports whether the mode is supported by the P1 policy contract.
func (m ApprovalMode) Valid() bool {
	return m == ApprovalAlwaysAsk || m == ApprovalAutoApproveDefaults || m == ApprovalYolo
}

// SkipsCommandConfirmation reports whether an already authorized command may execute without a user prompt.
func (m ApprovalMode) SkipsCommandConfirmation() bool {
	return m == ApprovalYolo
}

type ApprovalRecord struct {
	Source            ApprovalSource
	PolicyFingerprint string
	ApprovedAt        time.Time
}

func (a ApprovalRecord) Validate() error {
	if !a.Source.Valid() {
		return InvalidValue("approval.source", "unknown approval source")
	}
	if a.ApprovedAt.IsZero() {
		return InvalidValue("approval.approvedAt", "approval time is required")
	}
	if a.Source == ApprovalSourcePolicyDefault && EmptyID(a.PolicyFingerprint) {
		return InvalidValue("approval.policyFingerprint", "policy approval requires a fingerprint")
	}
	return nil
}

func NewUserApproval(at time.Time) ApprovalRecord {
	return ApprovalRecord{Source: ApprovalSourceUser, ApprovedAt: at.UTC()}
}

func NewPolicyApproval(fingerprint string, at time.Time) (ApprovalRecord, error) {
	if EmptyID(fingerprint) {
		return ApprovalRecord{}, InvalidValue("approval.policyFingerprint", "policy fingerprint is required")
	}
	return ApprovalRecord{
		Source:            ApprovalSourcePolicyDefault,
		PolicyFingerprint: fingerprint,
		ApprovedAt:        at.UTC(),
	}, nil
}
