package domain

import (
	"strings"
	"time"
)

type ExecutionReason string

const (
	ExecutionUserInput       ExecutionReason = "user_input"
	ExecutionQueuedWork      ExecutionReason = "queued_work"
	ExecutionContextDelivery ExecutionReason = "context_delivery"
	ExecutionResume          ExecutionReason = "resume"
)

type ExecutionStatus string

const (
	ExecutionStarting ExecutionStatus = "starting"
	ExecutionRunning  ExecutionStatus = "running"
	ExecutionSettling ExecutionStatus = "settling"
	ExecutionSettled  ExecutionStatus = "settled"
)

type ExecutionOutcome string

const (
	ExecutionCompleted   ExecutionOutcome = "completed"
	ExecutionYielded     ExecutionOutcome = "yielded"
	ExecutionPaused      ExecutionOutcome = "paused"
	ExecutionFailed      ExecutionOutcome = "failed"
	ExecutionInterrupted ExecutionOutcome = "interrupted"
)

// ExecutionInputSnapshot freezes all durable references used by one
// activation. Its values are never replaced once the execution is created.
type ExecutionInputSnapshot struct {
	TaskPacketID      TaskPacketID
	ContextManifestID ContextManifestID
	CapabilityGrantID CapabilityGrantID
	Runtime           RuntimeExecutionSnapshot
}

func (s ExecutionInputSnapshot) Validate() error {
	if idIsEmpty(string(s.TaskPacketID)) || idIsEmpty(string(s.ContextManifestID)) ||
		idIsEmpty(string(s.CapabilityGrantID)) {
		return invalidValue("executionInput", "required reference is missing")
	}
	if err := s.Runtime.Validate(); err != nil {
		return fmtField("executionInput.runtime", err)
	}
	return nil
}

type AgentExecution struct {
	ID                 AgentExecutionID
	SessionID          SessionID
	AgentID            AgentID
	RequestID          RequestID
	WorkItemID         WorkItemID
	Reason             ExecutionReason
	Status             ExecutionStatus
	StartContent       string
	StartContentDigest string
	Input              ExecutionInputSnapshot
	CreatedAt          time.Time
	StartedAt          time.Time
	SettledAt          time.Time
	Outcome            ExecutionOutcome
	FailureCode        ExecutionFailureCode
}

func NewAgentExecution(
	id AgentExecutionID,
	sessionID SessionID,
	agentID AgentID,
	requestID RequestID,
	reason ExecutionReason,
	startContent string,
	input ExecutionInputSnapshot,
	at time.Time,
) (AgentExecution, error) {
	execution := AgentExecution{
		ID:           id,
		SessionID:    sessionID,
		AgentID:      agentID,
		RequestID:    requestID,
		Reason:       reason,
		Status:       ExecutionStarting,
		StartContent: startContent,
		Input:        input,
		CreatedAt:    at.UTC(),
	}
	if err := execution.Validate(); err != nil {
		return AgentExecution{}, err
	}
	return execution, nil
}

// NewQueuedWorkExecution creates the one execution associated with a durable
// independent work item. The item remains the owner of its task body; this
// execution only carries the immutable activation snapshot and correlation.
func NewQueuedWorkExecution(
	id AgentExecutionID,
	sessionID SessionID,
	agentID AgentID,
	workItemID WorkItemID,
	input ExecutionInputSnapshot,
	at time.Time,
) (AgentExecution, error) {
	if idIsEmpty(string(workItemID)) {
		return AgentExecution{}, invalidValue("agentExecution.workItemID", "work item id is required")
	}
	execution := AgentExecution{
		ID:         id,
		SessionID:  sessionID,
		AgentID:    agentID,
		RequestID:  RequestID("work:" + workItemID.String()),
		WorkItemID: workItemID,
		Reason:     ExecutionQueuedWork,
		Status:     ExecutionStarting,
		Input:      input,
		CreatedAt:  at.UTC(),
	}
	if err := execution.Validate(); err != nil {
		return AgentExecution{}, err
	}
	return execution, nil
}

func (e AgentExecution) Validate() error {
	if idIsEmpty(string(e.ID)) || idIsEmpty(string(e.SessionID)) || idIsEmpty(string(e.AgentID)) ||
		idIsEmpty(string(e.RequestID)) {
		return invalidValue("agentExecution", "required reference is missing")
	}
	if !validExecutionReason(e.Reason) || !validExecutionStatus(e.Status) {
		return invalidValue("agentExecution", "unknown reason or status")
	}
	if e.Reason == ExecutionQueuedWork && idIsEmpty(string(e.WorkItemID)) {
		return invalidValue("agentExecution.workItemID", "queued work execution requires a work item")
	}
	if e.Reason != ExecutionQueuedWork && e.WorkItemID != "" {
		return invalidValue("agentExecution.workItemID", "only queued work executions may reference a work item")
	}
	if err := e.Input.Validate(); err != nil {
		return fmtField("agentExecution.input", err)
	}
	if e.CreatedAt.IsZero() {
		return invalidValue("agentExecution.createdAt", "creation time is required")
	}
	if (!e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt)) ||
		(!e.SettledAt.IsZero() && e.SettledAt.Before(e.CreatedAt)) {
		return invalidValue("agentExecution.timestamps", "timestamps cannot precede creation")
	}
	if e.Status == ExecutionStarting {
		if !e.StartedAt.IsZero() || !e.SettledAt.IsZero() || e.Outcome != "" {
			return invalidValue("agentExecution", "starting execution has terminal fields")
		}
		if e.Reason == ExecutionUserInput && strings.TrimSpace(e.StartContent) == "" {
			return invalidValue("agentExecution.startContent", "user input execution requires content")
		}
	}
	if e.Status == ExecutionRunning || e.Status == ExecutionSettling {
		if e.StartedAt.IsZero() || !e.SettledAt.IsZero() || e.Outcome != "" {
			return invalidValue("agentExecution", "active execution has invalid lifecycle fields")
		}
	}
	if e.Status == ExecutionSettled {
		if e.StartedAt.IsZero() || e.SettledAt.IsZero() || !validExecutionOutcome(e.Outcome) {
			return invalidValue("agentExecution", "settled execution has invalid terminal fields")
		}
		if e.Outcome == ExecutionFailed && e.FailureCode == "" {
			return invalidValue("agentExecution.failureCode", "failed execution requires a failure code")
		}
	}
	if !e.FailureCode.Valid() {
		return invalidValue("agentExecution.failureCode", "unknown failure code")
	}
	if e.StartContent == "" && e.StartContentDigest == "" && e.Reason == ExecutionUserInput {
		return invalidValue("agentExecution.startContent", "input receipt digest is required after content removal")
	}
	return nil
}

func (e AgentExecution) Active() bool {
	return e.Status == ExecutionStarting || e.Status == ExecutionRunning || e.Status == ExecutionSettling
}

func (e *AgentExecution) MarkRunning(at time.Time) error {
	if e.Status != ExecutionStarting {
		return invalidTransition("agentExecution", string(e.Status), string(ExecutionRunning))
	}
	e.Status = ExecutionRunning
	e.StartedAt = at.UTC()
	return nil
}

func (e *AgentExecution) BeginSettlement(at time.Time) error {
	if e.Status != ExecutionRunning {
		return invalidTransition("agentExecution", string(e.Status), string(ExecutionSettling))
	}
	e.Status = ExecutionSettling
	if at.Before(e.StartedAt) {
		return invalidValue("agentExecution.settlingAt", "settlement cannot precede start")
	}
	return nil
}

// ClearStartContent removes the temporary copy only after a durable session
// receipt makes the user input independently recoverable.
func (e *AgentExecution) ClearStartContent(digest string) error {
	if strings.TrimSpace(digest) == "" {
		return invalidValue("agentExecution.startContentDigest", "digest is required")
	}
	if e.StartContent == "" {
		return nil
	}
	e.StartContent = ""
	e.StartContentDigest = strings.TrimSpace(digest)
	return nil
}

func (e *AgentExecution) Settle(
	outcome ExecutionOutcome,
	failureCode ExecutionFailureCode,
	at time.Time,
) error {
	if e.Status != ExecutionRunning && e.Status != ExecutionSettling {
		return invalidTransition("agentExecution", string(e.Status), string(ExecutionSettled))
	}
	if !validExecutionOutcome(outcome) {
		return invalidValue("agentExecution.outcome", "unknown execution outcome")
	}
	if at.Before(e.StartedAt) {
		return invalidValue("agentExecution.settledAt", "settlement cannot precede start")
	}
	failureCode = ExecutionFailureCode(strings.TrimSpace(string(failureCode)))
	if !failureCode.Valid() || (outcome == ExecutionFailed && failureCode == "") {
		return invalidValue("agentExecution.failureCode", "unknown or missing failure code")
	}
	e.Status = ExecutionSettled
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.SettledAt = at.UTC()
	return nil
}

func validExecutionReason(reason ExecutionReason) bool {
	switch reason {
	case ExecutionUserInput, ExecutionQueuedWork, ExecutionContextDelivery, ExecutionResume:
		return true
	default:
		return false
	}
}

func validExecutionStatus(status ExecutionStatus) bool {
	switch status {
	case ExecutionStarting, ExecutionRunning, ExecutionSettling, ExecutionSettled:
		return true
	default:
		return false
	}
}

func validExecutionOutcome(outcome ExecutionOutcome) bool {
	switch outcome {
	case ExecutionCompleted, ExecutionYielded, ExecutionPaused, ExecutionFailed, ExecutionInterrupted:
		return true
	default:
		return false
	}
}
