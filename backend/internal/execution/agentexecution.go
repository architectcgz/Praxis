package execution

import (
	"praxis/internal/contracts"

	"path/filepath"
	"strings"
	"time"

	appcontext "praxis/internal/context"
)

type ExecutionReason string

const (
	ExecutionUserInput  ExecutionReason = "user_input"
	ExecutionQueuedWork ExecutionReason = "queued_work"
	ExecutionResume     ExecutionReason = "resume"
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

// ExecutionInputSnapshot 冻结一次 execution 激活所需的全部持久化输入。
type ExecutionInputSnapshot struct {
	AgentDefinitionID         contracts.AgentDefinitionID
	AgentDefinitionRevision   string
	ContextRevision           uint64
	ContextDigest             string
	TranscriptThroughSequence uint64
	Context                   appcontext.ExecutionContext
	Model                     contracts.ExecutionModelSnapshot
	WorkspacePath             string
	Security                  contracts.ExecutionSecuritySnapshot
	Runtime                   contracts.RuntimeExecutionSnapshot
}

func (s ExecutionInputSnapshot) Validate() error {
	if !s.AgentDefinitionID.Valid() || strings.TrimSpace(s.AgentDefinitionRevision) == "" {
		return contracts.InvalidValue("executionInput.agentDefinition", "definition id and revision are required")
	}
	if s.ContextRevision == 0 || strings.TrimSpace(s.ContextDigest) == "" || s.TranscriptThroughSequence == 0 {
		return contracts.InvalidValue("executionInput.context", "context revision, digest and transcript sequence are required")
	}
	if err := s.Context.Validate(); err != nil {
		return contracts.FieldError("executionInput.context", err)
	}
	digest, err := s.Context.Digest()
	if err != nil {
		return contracts.FieldError("executionInput.context", err)
	}
	if digest != s.ContextDigest {
		return contracts.InvalidValue("executionInput.contextDigest", "context digest does not match context")
	}
	if err := s.Model.Validate(); err != nil {
		return contracts.FieldError("executionInput.model", err)
	}
	cleanWorkspacePath := filepath.Clean(strings.TrimSpace(s.WorkspacePath))
	if cleanWorkspacePath == "." || !filepath.IsAbs(cleanWorkspacePath) || cleanWorkspacePath != s.WorkspacePath {
		return contracts.InvalidValue("executionInput.workspacePath", "workspace path must be an absolute normalized path")
	}
	if err := s.Security.Validate(); err != nil {
		return contracts.FieldError("executionInput.security", err)
	}
	if err := s.Runtime.Validate(); err != nil {
		return contracts.FieldError("executionInput.runtime", err)
	}
	if s.Runtime.SandboxMode != s.Security.Sandbox.Mode ||
		s.Runtime.ApprovalMode != s.Security.ApprovalRules[0].Mode ||
		s.Runtime.Revision != s.Security.Fingerprint {
		return contracts.InvalidValue("executionInput.runtime", "runtime constraints must match the security snapshot")
	}
	return nil
}

type AgentExecution struct {
	ID                 contracts.AgentExecutionID
	SessionID          contracts.SessionID
	AgentID            contracts.AgentID
	ParentExecutionID  contracts.AgentExecutionID
	ContextRevision    uint64
	RequestID          contracts.RequestID
	WorkItemID         contracts.WorkItemID
	Reason             ExecutionReason
	Status             ExecutionStatus
	StartContent       string
	StartContentDigest string
	Input              ExecutionInputSnapshot
	CreatedAt          time.Time
	StartedAt          time.Time
	SettledAt          time.Time
	Outcome            ExecutionOutcome
	FailureCode        contracts.ExecutionFailureCode
}

func NewAgentExecution(
	id contracts.AgentExecutionID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	requestID contracts.RequestID,
	reason ExecutionReason,
	startContent string,
	input ExecutionInputSnapshot,
	at time.Time,
) (AgentExecution, error) {
	execution := AgentExecution{
		ID: id, SessionID: sessionID, AgentID: agentID,
		ContextRevision: input.ContextRevision,
		RequestID:       requestID, Reason: reason, Status: ExecutionStarting,
		StartContent: startContent, Input: input, CreatedAt: at.UTC(),
	}
	if err := execution.Validate(); err != nil {
		return AgentExecution{}, err
	}
	return execution, nil
}

// NewQueuedWorkExecution 创建一个与持久化 work item 关联的 execution。
func NewQueuedWorkExecution(
	id contracts.AgentExecutionID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	workItemID contracts.WorkItemID,
	prompt string,
	input ExecutionInputSnapshot,
	at time.Time,
) (AgentExecution, error) {
	if contracts.EmptyID(string(workItemID)) {
		return AgentExecution{}, contracts.InvalidValue("agentExecution.workItemID", "work item id is required")
	}
	execution := AgentExecution{
		ID: id, SessionID: sessionID, AgentID: agentID,
		ContextRevision: input.ContextRevision,
		RequestID:       contracts.RequestID("work:" + workItemID.String()), WorkItemID: workItemID,
		Reason: ExecutionQueuedWork, Status: ExecutionStarting, StartContent: strings.TrimSpace(prompt),
		Input: input, CreatedAt: at.UTC(),
	}
	if err := execution.Validate(); err != nil {
		return AgentExecution{}, err
	}
	return execution, nil
}

func (e AgentExecution) Validate() error {
	if contracts.EmptyID(string(e.ID)) || contracts.EmptyID(string(e.SessionID)) || contracts.EmptyID(string(e.AgentID)) ||
		contracts.EmptyID(string(e.RequestID)) {
		return contracts.InvalidValue("agentExecution", "required reference is missing")
	}
	if !validExecutionReason(e.Reason) || !validExecutionStatus(e.Status) {
		return contracts.InvalidValue("agentExecution", "unknown reason or status")
	}
	if e.Reason == ExecutionQueuedWork && contracts.EmptyID(string(e.WorkItemID)) {
		return contracts.InvalidValue("agentExecution.workItemID", "queued work execution requires a work item")
	}
	if e.Reason == ExecutionQueuedWork && strings.TrimSpace(e.StartContent) == "" && e.StartContentDigest == "" {
		return contracts.InvalidValue("agentExecution.startContent", "queued work execution requires its task prompt")
	}
	if e.Reason != ExecutionQueuedWork && e.WorkItemID != "" {
		return contracts.InvalidValue("agentExecution.workItemID", "only queued work executions may reference a work item")
	}
	if err := e.Input.Validate(); err != nil {
		return contracts.FieldError("agentExecution.input", err)
	}
	if e.ContextRevision == 0 || e.ContextRevision != e.Input.ContextRevision {
		return contracts.InvalidValue("agentExecution.contextRevision", "context revision must match the frozen context")
	}
	if e.CreatedAt.IsZero() {
		return contracts.InvalidValue("agentExecution.createdAt", "creation time is required")
	}
	if (!e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt)) ||
		(!e.SettledAt.IsZero() && e.SettledAt.Before(e.CreatedAt)) {
		return contracts.InvalidValue("agentExecution.timestamps", "timestamps cannot precede creation")
	}
	if e.Status == ExecutionStarting {
		if !e.StartedAt.IsZero() || !e.SettledAt.IsZero() || e.Outcome != "" {
			return contracts.InvalidValue("agentExecution", "starting execution has terminal fields")
		}
		if e.Reason == ExecutionUserInput && strings.TrimSpace(e.StartContent) == "" {
			return contracts.InvalidValue("agentExecution.startContent", "user input execution requires content")
		}
	}
	if e.Status == ExecutionRunning || e.Status == ExecutionSettling {
		if e.StartedAt.IsZero() || !e.SettledAt.IsZero() || e.Outcome != "" {
			return contracts.InvalidValue("agentExecution", "active execution has invalid lifecycle fields")
		}
	}
	if e.Status == ExecutionSettled {
		if e.StartedAt.IsZero() || e.SettledAt.IsZero() || !validExecutionOutcome(e.Outcome) {
			return contracts.InvalidValue("agentExecution", "settled execution has invalid terminal fields")
		}
		if e.Outcome == ExecutionFailed && e.FailureCode == "" {
			return contracts.InvalidValue("agentExecution.failureCode", "failed execution requires a failure code")
		}
	}
	if !e.FailureCode.Valid() {
		return contracts.InvalidValue("agentExecution.failureCode", "unknown failure code")
	}
	if e.StartContent == "" && e.StartContentDigest == "" && e.Reason == ExecutionUserInput {
		return contracts.InvalidValue("agentExecution.startContent", "input receipt digest is required after content removal")
	}
	return nil
}

func (e AgentExecution) Active() bool {
	return e.Status == ExecutionStarting || e.Status == ExecutionRunning || e.Status == ExecutionSettling
}

func (e *AgentExecution) MarkRunning(at time.Time) error {
	if e.Status != ExecutionStarting {
		return contracts.InvalidTransition("agentExecution", string(e.Status), string(ExecutionRunning))
	}
	e.Status = ExecutionRunning
	e.StartedAt = at.UTC()
	return nil
}

func (e *AgentExecution) BeginSettlement(at time.Time) error {
	if e.Status != ExecutionRunning {
		return contracts.InvalidTransition("agentExecution", string(e.Status), string(ExecutionSettling))
	}
	e.Status = ExecutionSettling
	if at.Before(e.StartedAt) {
		return contracts.InvalidValue("agentExecution.settlingAt", "settlement cannot precede start")
	}
	return nil
}

// ClearStartContent 只有在 durable receipt 已经独立保存输入后才清理临时正文。
func (e *AgentExecution) ClearStartContent(digest string) error {
	if strings.TrimSpace(digest) == "" {
		return contracts.InvalidValue("agentExecution.startContentDigest", "digest is required")
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
	failureCode contracts.ExecutionFailureCode,
	at time.Time,
) error {
	if e.Status != ExecutionRunning && e.Status != ExecutionSettling {
		return contracts.InvalidTransition("agentExecution", string(e.Status), string(ExecutionSettled))
	}
	if !validExecutionOutcome(outcome) {
		return contracts.InvalidValue("agentExecution.outcome", "unknown execution outcome")
	}
	if at.Before(e.StartedAt) {
		return contracts.InvalidValue("agentExecution.settledAt", "settlement cannot precede start")
	}
	failureCode = contracts.ExecutionFailureCode(strings.TrimSpace(string(failureCode)))
	if !failureCode.Valid() || (outcome == ExecutionFailed && failureCode == "") {
		return contracts.InvalidValue("agentExecution.failureCode", "unknown or missing failure code")
	}
	e.Status = ExecutionSettled
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.SettledAt = at.UTC()
	return nil
}

func validExecutionReason(reason ExecutionReason) bool {
	switch reason {
	case ExecutionUserInput, ExecutionQueuedWork, ExecutionResume:
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
