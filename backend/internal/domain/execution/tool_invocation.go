package execution

import (
	"encoding/json"
	"strings"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

const MaxInlineToolResultBytes = 50 * 1024

type ToolInvocationStatus string

const (
	ToolInvocationRequested        ToolInvocationStatus = "requested"
	ToolInvocationAwaitingApproval ToolInvocationStatus = "awaiting_approval"
	ToolInvocationApproved         ToolInvocationStatus = "approved"
	ToolInvocationRunning          ToolInvocationStatus = "running"
	ToolInvocationSucceeded        ToolInvocationStatus = "succeeded"
	ToolInvocationFailed           ToolInvocationStatus = "failed"
	ToolInvocationDenied           ToolInvocationStatus = "denied"
	ToolInvocationInterrupted      ToolInvocationStatus = "interrupted"
	ToolInvocationUnknown          ToolInvocationStatus = "unknown"
)

type ToolFailureCode string

const (
	ToolFailureNone            ToolFailureCode = ""
	ToolFailureNotAllowed      ToolFailureCode = "tool_not_allowed"
	ToolFailureApprovalDenied  ToolFailureCode = "tool_approval_denied"
	ToolFailureExecutionFailed ToolFailureCode = "tool_execution_failed"
	ToolFailureResultUnknown   ToolFailureCode = "tool_result_unknown"
	ToolFailureInterrupted     ToolFailureCode = "tool_interrupted"
)

type ToolInvocationResult struct {
	InlineContent string
	ErrorCode     ToolFailureCode
	SideEffect    bool
	Truncated     bool
}

func (r ToolInvocationResult) Validate() error {
	if len([]byte(r.InlineContent)) > MaxInlineToolResultBytes {
		return invalidValue("toolInvocation.result.inlineContent", "inline result exceeds the size limit")
	}
	if !r.ErrorCode.Valid() {
		return invalidValue("toolInvocation.result.errorCode", "unknown tool failure code")
	}
	return nil
}

func (c ToolFailureCode) Valid() bool {
	switch c {
	case ToolFailureNone, ToolFailureNotAllowed, ToolFailureApprovalDenied,
		ToolFailureExecutionFailed, ToolFailureResultUnknown, ToolFailureInterrupted:
		return true
	default:
		return false
	}
}

// ToolInvocation is the durable execution-owned identity for one provider tool call.
type ToolInvocation struct {
	ID                  domainfoundation.ToolInvocationID
	ExecutionID         AgentExecutionID
	SessionID           SessionID
	AgentID             AgentID
	ProviderToolCallID  string
	Name                domainsecurity.ToolName
	NormalizedArguments json.RawMessage
	ArgumentsDigest     string
	Status              ToolInvocationStatus
	Approval            domainsecurity.ApprovalRecord
	Result              ToolInvocationResult
	FailureCode         ToolFailureCode
	CreatedAt           time.Time
	ApprovedAt          time.Time
	StartedAt           time.Time
	SettledAt           time.Time
}

func NewToolInvocation(
	id domainfoundation.ToolInvocationID,
	executionID AgentExecutionID,
	sessionID SessionID,
	agentID AgentID,
	providerToolCallID string,
	name domainsecurity.ToolName,
	normalizedArguments json.RawMessage,
	argumentsDigest string,
	at time.Time,
) (ToolInvocation, error) {
	invocation := ToolInvocation{
		ID:                  id,
		ExecutionID:         executionID,
		SessionID:           sessionID,
		AgentID:             agentID,
		ProviderToolCallID:  strings.TrimSpace(providerToolCallID),
		Name:                name,
		NormalizedArguments: cloneJSON(normalizedArguments),
		ArgumentsDigest:     strings.TrimSpace(argumentsDigest),
		Status:              ToolInvocationRequested,
		CreatedAt:           at.UTC(),
	}
	if err := invocation.Validate(); err != nil {
		return ToolInvocation{}, err
	}
	return invocation, nil
}

func (i ToolInvocation) Validate() error {
	if idIsEmpty(i.ID.String()) || idIsEmpty(string(i.ExecutionID)) ||
		idIsEmpty(string(i.SessionID)) || idIsEmpty(string(i.AgentID)) {
		return invalidValue("toolInvocation", "required identity is missing")
	}
	if strings.TrimSpace(i.ProviderToolCallID) == "" || !i.Name.Valid() {
		return invalidValue("toolInvocation", "provider call identity and tool name are required")
	}
	if len(i.NormalizedArguments) == 0 || !json.Valid(i.NormalizedArguments) ||
		strings.TrimSpace(i.ArgumentsDigest) == "" {
		return invalidValue("toolInvocation", "normalized arguments and digest are required")
	}
	if !i.Status.Valid() || !i.FailureCode.Valid() || i.CreatedAt.IsZero() {
		return invalidValue("toolInvocation", "status, failure code, or creation time is invalid")
	}
	if err := i.Result.Validate(); err != nil {
		return err
	}
	if (!i.ApprovedAt.IsZero() && i.ApprovedAt.Before(i.CreatedAt)) ||
		(!i.StartedAt.IsZero() && (i.ApprovedAt.IsZero() || i.StartedAt.Before(i.ApprovedAt))) ||
		(!i.SettledAt.IsZero() && i.SettledAt.Before(i.CreatedAt)) {
		return invalidValue("toolInvocation.timestamps", "lifecycle timestamps are out of order")
	}
	if i.Status == ToolInvocationRequested || i.Status == ToolInvocationAwaitingApproval {
		if !i.ApprovedAt.IsZero() || !i.StartedAt.IsZero() || !i.SettledAt.IsZero() ||
			i.FailureCode != "" || i.Result != (ToolInvocationResult{}) {
			return invalidValue("toolInvocation", "unapproved invocation contains later lifecycle fields")
		}
	}
	if i.Status == ToolInvocationApproved &&
		(i.ApprovedAt.IsZero() || !i.StartedAt.IsZero() || !i.SettledAt.IsZero() ||
			i.FailureCode != "" || i.Result != (ToolInvocationResult{})) {
		return invalidValue("toolInvocation", "approved invocation has invalid timestamps")
	}
	if i.Status == ToolInvocationRunning &&
		(i.ApprovedAt.IsZero() || i.StartedAt.IsZero() || !i.SettledAt.IsZero() ||
			i.FailureCode != "" || i.Result != (ToolInvocationResult{})) {
		return invalidValue("toolInvocation", "running invocation has invalid timestamps")
	}
	if i.Status.Terminal() && i.SettledAt.IsZero() {
		return invalidValue("toolInvocation", "settled invocation requires a settlement time")
	}
	if !i.SettledAt.IsZero() && !i.StartedAt.IsZero() && i.SettledAt.Before(i.StartedAt) {
		return invalidValue("toolInvocation.settledAt", "settlement cannot precede start")
	}
	switch i.Status {
	case ToolInvocationSucceeded:
		if i.ApprovedAt.IsZero() || i.StartedAt.IsZero() || i.FailureCode != "" || i.Result.ErrorCode != "" {
			return invalidValue("toolInvocation", "successful invocation has inconsistent result fields")
		}
	case ToolInvocationFailed:
		if i.ApprovedAt.IsZero() || i.StartedAt.IsZero() || i.FailureCode == "" || i.Result.ErrorCode != i.FailureCode {
			return invalidValue("toolInvocation", "failed invocation has inconsistent result fields")
		}
	case ToolInvocationDenied:
		if !i.ApprovedAt.IsZero() || !i.StartedAt.IsZero() || i.Result.SideEffect ||
			(i.FailureCode != ToolFailureNotAllowed && i.FailureCode != ToolFailureApprovalDenied) ||
			i.Result.ErrorCode != i.FailureCode {
			return invalidValue("toolInvocation", "denied invocation has inconsistent result fields")
		}
	case ToolInvocationInterrupted:
		if i.FailureCode != ToolFailureInterrupted || i.Result.ErrorCode != i.FailureCode {
			return invalidValue("toolInvocation", "interrupted invocation has inconsistent result fields")
		}
	}
	if !i.ApprovedAt.IsZero() {
		if err := i.Approval.Validate(); err != nil {
			return invalidValue("toolInvocation.approval", err.Error())
		}
	}
	return nil
}

func (s ToolInvocationStatus) Valid() bool {
	switch s {
	case ToolInvocationRequested, ToolInvocationAwaitingApproval, ToolInvocationApproved,
		ToolInvocationRunning, ToolInvocationSucceeded, ToolInvocationFailed,
		ToolInvocationDenied, ToolInvocationInterrupted, ToolInvocationUnknown:
		return true
	default:
		return false
	}
}

func (s ToolInvocationStatus) Terminal() bool {
	return s == ToolInvocationSucceeded || s == ToolInvocationFailed ||
		s == ToolInvocationDenied || s == ToolInvocationInterrupted
}

func (i *ToolInvocation) Approve(approval domainsecurity.ApprovalRecord) error {
	if i.Status != ToolInvocationRequested && i.Status != ToolInvocationAwaitingApproval {
		return invalidTransition("toolInvocation", string(i.Status), string(ToolInvocationApproved))
	}
	if err := approval.Validate(); err != nil {
		return err
	}
	i.Status = ToolInvocationApproved
	i.Approval = approval
	i.ApprovedAt = approval.ApprovedAt.UTC()
	return nil
}

func (i *ToolInvocation) Start(at time.Time) error {
	if i.Status != ToolInvocationApproved {
		return invalidTransition("toolInvocation", string(i.Status), string(ToolInvocationRunning))
	}
	if at.Before(i.ApprovedAt) {
		return invalidValue("toolInvocation.startedAt", "start cannot precede approval")
	}
	i.Status = ToolInvocationRunning
	i.StartedAt = at.UTC()
	return nil
}

func (i *ToolInvocation) Succeed(result ToolInvocationResult, at time.Time) error {
	if result.ErrorCode != "" {
		return invalidValue("toolInvocation.result", "successful result cannot contain an error code")
	}
	return i.settle(ToolInvocationSucceeded, result, ToolFailureNone, at)
}

func (i *ToolInvocation) Fail(result ToolInvocationResult, code ToolFailureCode, at time.Time) error {
	if code == "" || result.ErrorCode != code {
		return invalidValue("toolInvocation.result", "failed result must match its failure code")
	}
	return i.settle(ToolInvocationFailed, result, code, at)
}

func (i *ToolInvocation) Deny(result ToolInvocationResult, at time.Time) error {
	if i.Status != ToolInvocationRequested && i.Status != ToolInvocationAwaitingApproval {
		return invalidTransition("toolInvocation", string(i.Status), string(ToolInvocationDenied))
	}
	if result.ErrorCode != ToolFailureNotAllowed && result.ErrorCode != ToolFailureApprovalDenied {
		return invalidValue("toolInvocation.result", "denial requires a denial failure code")
	}
	if err := result.Validate(); err != nil {
		return err
	}
	i.Status = ToolInvocationDenied
	i.Result = result
	i.FailureCode = result.ErrorCode
	i.SettledAt = at.UTC()
	return nil
}

func (i *ToolInvocation) Interrupt(result ToolInvocationResult, at time.Time) error {
	if i.Status != ToolInvocationRunning && i.Status != ToolInvocationAwaitingApproval {
		return invalidTransition("toolInvocation", string(i.Status), string(ToolInvocationInterrupted))
	}
	if result.ErrorCode != ToolFailureInterrupted {
		return invalidValue("toolInvocation.result", "interruption requires its stable failure code")
	}
	if err := result.Validate(); err != nil {
		return err
	}
	i.Status = ToolInvocationInterrupted
	i.Result = result
	i.FailureCode = ToolFailureInterrupted
	i.SettledAt = at.UTC()
	return nil
}

func (i *ToolInvocation) settle(
	status ToolInvocationStatus,
	result ToolInvocationResult,
	code ToolFailureCode,
	at time.Time,
) error {
	if i.Status != ToolInvocationRunning {
		return invalidTransition("toolInvocation", string(i.Status), string(status))
	}
	if at.Before(i.StartedAt) {
		return invalidValue("toolInvocation.settledAt", "settlement cannot precede start")
	}
	if err := result.Validate(); err != nil {
		return err
	}
	i.Status = status
	i.Result = result
	i.FailureCode = code
	i.SettledAt = at.UTC()
	return nil
}

func (i ToolInvocation) Snapshot() ToolInvocation {
	i.NormalizedArguments = cloneJSON(i.NormalizedArguments)
	return i
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
