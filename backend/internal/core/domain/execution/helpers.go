package execution

import (
	"strings"

	domaincontext "praxis/internal/core/domain/context"
	foundation "praxis/internal/core/domain/foundation"
	"praxis/internal/core/domain/security"
)

type (
	AgentID                   = foundation.AgentID
	AgentExecutionID          = foundation.AgentExecutionID
	RequestID                 = foundation.RequestID
	SessionID                 = foundation.SessionID
	WorkItemID                = foundation.WorkItemID
	ContextManifest           = domaincontext.ContextManifest
	CapabilityGrant           = security.CapabilityGrant
	ExecutionSecuritySnapshot = security.ExecutionSecuritySnapshot
	SandboxMode               = security.SandboxMode
	ApprovalMode              = security.ApprovalMode
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func invalidTransition(entity, from, to string) error {
	return &foundation.TransitionError{Entity: entity, From: from, To: to}
}

func fmtField(field string, err error) error {
	if err == nil {
		return nil
	}
	return invalidValue(field, strings.TrimPrefix(err.Error(), field+": "))
}

// ValidExecutionOutcome reports whether an execution outcome is supported.
func ValidExecutionOutcome(outcome ExecutionOutcome) bool { return validExecutionOutcome(outcome) }
