package foundation

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidValue      = errors.New("invalid domain value")
	ErrInvalidTransition = errors.New("invalid domain transition")
	ErrLeaseConflict     = errors.New("workspace write lease conflict")
	ErrAlreadySettled    = errors.New("agent execution is already settled")
	ErrWorkQueueEmpty    = errors.New("work queue is empty")
	ErrWorkItemActive    = errors.New("agent already has an active work item")
	ErrAlreadyDelivered  = errors.New("briefing already has a delivery")
	ErrAgentExecuting    = errors.New("agent is executing")
	ErrAgentUnavailable  = errors.New("agent cannot accept this command")
	ErrRequestNotFound   = errors.New("execution request was not found")
	ErrRequestConflict   = errors.New("execution request conflict")
	ErrRevisionConflict  = errors.New("revision conflict")
	ErrNotFound          = errors.New("domain object not found")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error { return ErrInvalidValue }

func invalidValue(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

type TransitionError struct {
	Entity string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s cannot transition from %q to %q", e.Entity, e.From, e.To)
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }

func invalidTransition(entity, from, to string) error {
	return &TransitionError{Entity: entity, From: from, To: to}
}
