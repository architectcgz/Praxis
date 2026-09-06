package agent

import (
	"strings"

	"praxis/internal/domain/execution"
	foundation "praxis/internal/domain/foundation"
	"praxis/internal/domain/security"
)

type (
	AgentID          = foundation.AgentID
	AgentExecutionID = foundation.AgentExecutionID
	SessionID        = foundation.SessionID
	AgentProfile     = security.AgentProfile
	ExecutionOutcome = execution.ExecutionOutcome
)

const (
	ExecutionCompleted   = execution.ExecutionCompleted
	ExecutionYielded     = execution.ExecutionYielded
	ExecutionPaused      = execution.ExecutionPaused
	ExecutionFailed      = execution.ExecutionFailed
	ExecutionInterrupted = execution.ExecutionInterrupted
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func invalidTransition(entity, from, to string) error {
	return &foundation.TransitionError{Entity: entity, From: from, To: to}
}

func validExecutionOutcome(outcome ExecutionOutcome) bool {
	return execution.ValidExecutionOutcome(outcome)
}
