package turn

import (
	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"

	"strings"
	"time"

	appcontext "praxis/internal/core/context"
)

type TurnReason string

const (
	TurnUserInput  TurnReason = "user_input"
	TurnQueuedWork TurnReason = "queued_work"
	TurnResume     TurnReason = "resume"
)

type TurnStatus string

const (
	TurnStarting TurnStatus = "starting"
	TurnRunning  TurnStatus = "running"
	TurnEnding   TurnStatus = "ending"
	TurnEnded    TurnStatus = "ended"
)

type TurnOutcome string

const (
	TurnCompleted   TurnOutcome = "completed"
	TurnYielded     TurnOutcome = "yielded"
	TurnPaused      TurnOutcome = "paused"
	TurnFailed      TurnOutcome = "failed"
	TurnInterrupted TurnOutcome = "interrupted"
)

// InputSnapshot 冻结一次 turn 激活所需的全部持久化输入。
type InputSnapshot struct {
	AgentDefinitionID       contracts.AgentDefinitionID
	AgentDefinitionRevision string
	ContextDigest           string
	MessageSequenceBoundary uint64 `json:"messageSequenceBoundary"`
	CurrentInputMessageID   string `json:"currentInputMessageId"`
	Context                 appcontext.ModelContext
	Model                   contracts.ModelSnapshot
	WorkspacePath           string
	Security                contracts.SecuritySnapshot
}

func (s InputSnapshot) Validate() error {
	if !s.AgentDefinitionID.Valid() || s.AgentDefinitionRevision == "" ||
		s.AgentDefinitionRevision != strings.TrimSpace(s.AgentDefinitionRevision) {
		return contracts.InvalidValue("turnInput.agentDefinition", "definition id and revision are required")
	}
	if s.ContextDigest == "" || s.ContextDigest != strings.TrimSpace(s.ContextDigest) {
		return contracts.InvalidValue("turnInput.context", "context digest is required")
	}
	if err := s.Context.Validate(); err != nil {
		return contracts.FieldError("turnInput.context", err)
	}
	digest, err := s.Context.Digest()
	if err != nil {
		return contracts.FieldError("turnInput.context", err)
	}
	if digest != s.ContextDigest {
		return contracts.InvalidValue("turnInput.contextDigest", "context digest does not match context")
	}
	if err := s.Model.Validate(); err != nil {
		return contracts.FieldError("turnInput.model", err)
	}
	if !pathutil.IsAbsoluteNormalized(s.WorkspacePath) {
		return contracts.InvalidValue("turnInput.workspacePath", "workspace path must be an absolute normalized path")
	}
	if err := s.Security.Validate(); err != nil {
		return contracts.FieldError("turnInput.security", err)
	}
	return nil
}

type Turn struct {
	ID             contracts.TurnID
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	ParentTurnID   contracts.TurnID
	RequestID      contracts.RequestID
	WorkItemID     contracts.WorkItemID
	Reason         TurnReason
	Status         TurnStatus
	StartContent   string
	Input          InputSnapshot
	CreatedAt      time.Time
	StartedAt      time.Time
	EndedAt        time.Time
	Outcome        TurnOutcome
	FailureCode    contracts.TurnFailureCode
	FailureMessage string
}

func NewTurn(
	id contracts.TurnID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	requestID contracts.RequestID,
	reason TurnReason,
	startContent string,
	input InputSnapshot,
	at time.Time,
) (Turn, error) {
	turn := Turn{
		ID: id, SessionID: sessionID, AgentID: agentID,
		RequestID: requestID, Reason: reason, Status: TurnStarting,
		StartContent: startContent, Input: input, CreatedAt: at.UTC(),
	}
	if err := turn.Validate(); err != nil {
		return Turn{}, err
	}
	return turn, nil
}

// NewQueuedWorkTurn 创建一个关联到排队用户输入消息的执行。
func NewQueuedWorkTurn(
	id contracts.TurnID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	workItemID contracts.WorkItemID,
	requestID contracts.RequestID,
	input InputSnapshot,
	at time.Time,
) (Turn, error) {
	if contracts.EmptyID(string(workItemID)) {
		return Turn{}, contracts.InvalidValue("turn.workItemID", "work item id is required")
	}
	if contracts.EmptyID(string(requestID)) {
		return Turn{}, contracts.InvalidValue("turn.requestID", "request id is required")
	}
	turn := Turn{
		ID: id, SessionID: sessionID, AgentID: agentID,
		RequestID: requestID, WorkItemID: workItemID,
		Reason: TurnQueuedWork, Status: TurnStarting,
		Input: input, CreatedAt: at.UTC(),
	}
	if err := turn.Validate(); err != nil {
		return Turn{}, err
	}
	return turn, nil
}

func (e Turn) Validate() error {
	if contracts.EmptyID(string(e.ID)) || contracts.EmptyID(string(e.SessionID)) || contracts.EmptyID(string(e.AgentID)) ||
		contracts.EmptyID(string(e.RequestID)) {
		return contracts.InvalidValue("turn", "required reference is missing")
	}
	if !validTurnReason(e.Reason) || !validTurnStatus(e.Status) {
		return contracts.InvalidValue("turn", "unknown reason or status")
	}
	if e.Reason == TurnQueuedWork && contracts.EmptyID(string(e.WorkItemID)) {
		return contracts.InvalidValue("turn.workItemID", "queued work turn requires a work item")
	}
	if e.Reason != TurnQueuedWork && e.WorkItemID != "" {
		return contracts.InvalidValue("turn.workItemID", "only queued work turns may reference a work item")
	}
	if err := e.Input.Validate(); err != nil {
		return contracts.FieldError("turn.input", err)
	}
	if e.CreatedAt.IsZero() {
		return contracts.InvalidValue("turn.createdAt", "creation time is required")
	}
	if (!e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt)) ||
		(!e.EndedAt.IsZero() && e.EndedAt.Before(e.CreatedAt)) {
		return contracts.InvalidValue("turn.timestamps", "timestamps cannot precede creation")
	}
	if e.StartContent != strings.TrimSpace(e.StartContent) {
		return contracts.InvalidValue("turn.startContent", "input content must be normalized")
	}
	if e.Reason == TurnUserInput && e.StartContent == "" && e.Input.CurrentInputMessageID == "" {
		return contracts.InvalidValue("turn.startContent", "user input turn requires content or an input message reference")
	}
	if e.Status == TurnStarting {
		if !e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "" {
			return contracts.InvalidValue("turn", "starting turn has terminal fields")
		}
		if e.Reason == TurnUserInput && e.StartContent == "" {
			return contracts.InvalidValue("turn.startContent", "user input turn requires content")
		}
	}
	if e.Status == TurnRunning || e.Status == TurnEnding {
		if e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "" {
			return contracts.InvalidValue("turn", "active turn has invalid lifecycle fields")
		}
	}
	if e.Status == TurnEnded {
		if e.StartedAt.IsZero() || e.EndedAt.IsZero() || !validTurnOutcome(e.Outcome) {
			return contracts.InvalidValue("turn", "ended turn has invalid terminal fields")
		}
		if e.Outcome == TurnFailed && e.FailureCode == "" {
			return contracts.InvalidValue("turn.failureCode", "failed turn requires a failure code")
		}
	}
	if !e.FailureCode.Valid() {
		return contracts.InvalidValue("turn.failureCode", "unknown failure code")
	}
	if e.FailureCode == contracts.TurnFailureRequestCanceled && e.Outcome != TurnPaused && e.Outcome != TurnInterrupted {
		return contracts.InvalidValue("turn.failureCode", "request cancellation requires a paused or interrupted outcome")
	}
	if e.FailureMessage != strings.TrimSpace(e.FailureMessage) ||
		len([]rune(e.FailureMessage)) > contracts.MaxTurnFailureMessageRunes {
		return contracts.InvalidValue("turn.failureMessage", "failure message must be normalized and bounded")
	}
	if e.Status != TurnEnded && e.FailureMessage != "" {
		return contracts.InvalidValue("turn.failureMessage", "active turn cannot have a failure message")
	}
	if e.Status == TurnEnded && e.Outcome != TurnFailed && e.FailureMessage != "" {
		return contracts.InvalidValue("turn.failureMessage", "only failed turn may have a failure message")
	}
	return nil
}

func (e Turn) Active() bool {
	return e.Status == TurnStarting || e.Status == TurnRunning || e.Status == TurnEnding
}

func (e *Turn) MarkRunning(at time.Time) error {
	if e.Status != TurnStarting {
		return contracts.InvalidTransition("turn", string(e.Status), string(TurnRunning))
	}
	e.Status = TurnRunning
	e.StartedAt = at.UTC()
	return nil
}

// BeginEnding 标记执行已返回；结束时间不得早于启动时间，失败时不改变状态。
func (e *Turn) BeginEnding(at time.Time) error {
	if e.Status != TurnRunning {
		return contracts.InvalidTransition("turn", string(e.Status), string(TurnEnding))
	}
	if at.Before(e.StartedAt) {
		return contracts.InvalidValue("turn.endingAt", "ending cannot precede start")
	}
	e.Status = TurnEnding
	return nil
}

// End 固定最终结果和结束时间；已结束的 Turn 不允许重新结束或改变结果。
func (e *Turn) End(
	outcome TurnOutcome,
	failureCode contracts.TurnFailureCode,
	at time.Time,
) error {
	if e.Status != TurnRunning && e.Status != TurnEnding {
		return contracts.InvalidTransition("turn", string(e.Status), string(TurnEnded))
	}
	if !validTurnOutcome(outcome) {
		return contracts.InvalidValue("turn.outcome", "unknown turn outcome")
	}
	if at.Before(e.StartedAt) {
		return contracts.InvalidValue("turn.endedAt", "ending cannot precede start")
	}
	if !failureCode.Valid() || (outcome == TurnFailed && failureCode == "") {
		return contracts.InvalidValue("turn.failureCode", "unknown or missing failure code")
	}
	if failureCode == contracts.TurnFailureRequestCanceled && outcome != TurnPaused && outcome != TurnInterrupted {
		return contracts.InvalidValue("turn.failureCode", "request cancellation requires a paused or interrupted outcome")
	}
	e.Status = TurnEnded
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.EndedAt = at.UTC()
	return nil
}

func validTurnReason(reason TurnReason) bool {
	switch reason {
	case TurnUserInput, TurnQueuedWork, TurnResume:
		return true
	default:
		return false
	}
}

func validTurnStatus(status TurnStatus) bool {
	switch status {
	case TurnStarting, TurnRunning, TurnEnding, TurnEnded:
		return true
	default:
		return false
	}
}

func validTurnOutcome(outcome TurnOutcome) bool {
	switch outcome {
	case TurnCompleted, TurnYielded, TurnPaused, TurnFailed, TurnInterrupted:
		return true
	default:
		return false
	}
}
