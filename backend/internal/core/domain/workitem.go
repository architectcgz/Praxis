package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxWorkItemPromptBytes      = 32 * 1024
	MaxWorkItemFailureCodeBytes = 128
)

// WorkItemStatus describes the durable lifecycle of one independent task.
type WorkItemStatus string

const (
	WorkItemQueued      WorkItemStatus = "queued"
	WorkItemRunning     WorkItemStatus = "running"
	WorkItemPaused      WorkItemStatus = "paused"
	WorkItemFailed      WorkItemStatus = "failed"
	WorkItemInterrupted WorkItemStatus = "interrupted"
	WorkItemCompleted   WorkItemStatus = "completed"
	WorkItemCancelled   WorkItemStatus = "cancelled"
)

// QueuedWorkItem is a durable independent task scheduled for one AgentThread.
// Its execution snapshot is captured before enqueue so policy changes cannot
// widen permissions while the item waits for the thread's FIFO turn.
type QueuedWorkItem struct {
	ID                WorkItemID
	TaskSessionID     TaskSessionID
	AgentThreadID     AgentThreadID
	Sequence          uint64
	Prompt            string
	TaskPacketID      TaskPacketID
	ContextManifestID ContextManifestID
	CapabilityGrantID CapabilityGrantID
	Execution         RuntimeExecutionSnapshot
	Status            WorkItemStatus
	AgentRunID        AgentRunID
	CreatedAt         time.Time
	StartedAt         time.Time
	FinishedAt        time.Time
	FailureCode       string
}

// NewQueuedWorkItem creates an item in the durable FIFO queue.
func NewQueuedWorkItem(id WorkItemID, sessionID TaskSessionID, threadID AgentThreadID, sequence uint64, prompt string, packetID TaskPacketID, manifestID ContextManifestID, grantID CapabilityGrantID, execution RuntimeExecutionSnapshot, createdAt time.Time) (QueuedWorkItem, error) {
	item := QueuedWorkItem{
		ID:                id,
		TaskSessionID:     sessionID,
		AgentThreadID:     threadID,
		Sequence:          sequence,
		Prompt:            strings.TrimSpace(prompt),
		TaskPacketID:      packetID,
		ContextManifestID: manifestID,
		CapabilityGrantID: grantID,
		Execution:         execution.Snapshot(),
		Status:            WorkItemQueued,
		CreatedAt:         createdAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return QueuedWorkItem{}, err
	}
	return item, nil
}

// Validate checks references, timestamps and status-owned fields.
func (w QueuedWorkItem) Validate() error {
	if idIsEmpty(string(w.ID)) || idIsEmpty(string(w.TaskSessionID)) || idIsEmpty(string(w.AgentThreadID)) {
		return invalidValue("workItem", "required reference is missing")
	}
	if w.Sequence == 0 {
		return invalidValue("workItem.sequence", "sequence must be positive")
	}
	if w.Prompt == "" || len([]byte(w.Prompt)) > MaxWorkItemPromptBytes || !utf8.ValidString(w.Prompt) {
		return invalidValue("workItem.prompt", "prompt is empty, invalid UTF-8, or exceeds the bounded limit")
	}
	if idIsEmpty(string(w.TaskPacketID)) || idIsEmpty(string(w.ContextManifestID)) || idIsEmpty(string(w.CapabilityGrantID)) {
		return invalidValue("workItem", "execution references are required")
	}
	if err := w.Execution.Validate(); err != nil {
		return fmtField("workItem.execution", err)
	}
	if !validWorkItemStatus(w.Status) {
		return invalidValue("workItem.status", "unknown work item status")
	}
	if w.CreatedAt.IsZero() {
		return invalidValue("workItem.createdAt", "createdAt is required")
	}
	if !w.StartedAt.IsZero() && w.StartedAt.Before(w.CreatedAt) {
		return invalidValue("workItem.startedAt", "startedAt cannot precede createdAt")
	}
	if !w.FinishedAt.IsZero() && w.FinishedAt.Before(w.CreatedAt) {
		return invalidValue("workItem.finishedAt", "finishedAt cannot precede createdAt")
	}
	if w.StartedAt.IsZero() && !w.FinishedAt.IsZero() {
		return invalidValue("workItem.timestamps", "finished item must have a start time")
	}
	if w.FailureCode != "" && (len([]byte(w.FailureCode)) > MaxWorkItemFailureCodeBytes || strings.ContainsAny(w.FailureCode, "\x00\r\n")) {
		return invalidValue("workItem.failureCode", "failure code is invalid or exceeds the bounded limit")
	}

	switch w.Status {
	case WorkItemQueued:
		if w.AgentRunID != "" || !w.StartedAt.IsZero() || !w.FinishedAt.IsZero() || w.FailureCode != "" {
			return invalidValue("workItem.queued", "queued item cannot have run or settlement fields")
		}
	case WorkItemRunning:
		if idIsEmpty(string(w.AgentRunID)) || w.StartedAt.IsZero() || !w.FinishedAt.IsZero() || w.FailureCode != "" {
			return invalidValue("workItem.running", "running item must have only an active run")
		}
	case WorkItemPaused, WorkItemInterrupted, WorkItemFailed, WorkItemCompleted:
		if idIsEmpty(string(w.AgentRunID)) || w.StartedAt.IsZero() || w.FinishedAt.IsZero() {
			return invalidValue("workItem.settled", "settled item must have a run and timestamps")
		}
	case WorkItemCancelled:
		if w.AgentRunID != "" || !w.StartedAt.IsZero() || w.FinishedAt.IsZero() || w.FailureCode != "" {
			return invalidValue("workItem.cancelled", "cancelled item cannot have an active run")
		}
	}
	return nil
}

// Claim moves a queued item to running and binds the current AgentRun.
func (w *QueuedWorkItem) Claim(runID AgentRunID, at time.Time) (DomainEvent, error) {
	if w.Status != WorkItemQueued {
		return DomainEvent{}, invalidTransition("workItem", string(w.Status), string(WorkItemRunning))
	}
	if idIsEmpty(string(runID)) {
		return DomainEvent{}, invalidValue("workItem.agentRunID", "run id is required")
	}
	if at.Before(w.CreatedAt) {
		return DomainEvent{}, invalidValue("workItem.startedAt", "start time cannot precede creation")
	}
	w.Status = WorkItemRunning
	w.AgentRunID = runID
	w.StartedAt = at.UTC()
	w.FinishedAt = time.Time{}
	w.FailureCode = ""
	return w.event(EventWorkItemStarted, at), nil
}

// Resume starts a new AgentRun for a paused or interrupted item.
func (w *QueuedWorkItem) Resume(runID AgentRunID, at time.Time) (DomainEvent, error) {
	if w.Status != WorkItemPaused && w.Status != WorkItemInterrupted {
		return DomainEvent{}, invalidTransition("workItem", string(w.Status), string(WorkItemRunning))
	}
	if idIsEmpty(string(runID)) {
		return DomainEvent{}, invalidValue("workItem.agentRunID", "run id is required")
	}
	if at.Before(w.CreatedAt) {
		return DomainEvent{}, invalidValue("workItem.startedAt", "resume time cannot precede creation")
	}
	w.Status = WorkItemRunning
	w.AgentRunID = runID
	w.StartedAt = at.UTC()
	w.FinishedAt = time.Time{}
	w.FailureCode = ""
	event := w.event(EventWorkItemResumed, at)
	event.Payload["runId"] = runID.String()
	return event, nil
}

// Settle records the outcome of the currently bound AgentRun.
func (w *QueuedWorkItem) Settle(runID AgentRunID, outcome AgentRunOutcome, at time.Time, failureCode string) (DomainEvent, error) {
	if w.Status != WorkItemRunning {
		return DomainEvent{}, invalidTransition("workItem", string(w.Status), string(statusForOutcome(outcome)))
	}
	if w.AgentRunID != runID {
		return DomainEvent{}, invalidValue("workItem.agentRunID", "settlement run does not own the item")
	}
	if !validRunOutcome(outcome) {
		return DomainEvent{}, invalidValue("workItem.outcome", "unknown run outcome")
	}
	if at.Before(w.CreatedAt) || at.Before(w.StartedAt) {
		return DomainEvent{}, invalidValue("workItem.finishedAt", "settlement time cannot precede start")
	}
	failureCode = strings.TrimSpace(failureCode)
	if failureCode != "" && (len([]byte(failureCode)) > MaxWorkItemFailureCodeBytes || strings.ContainsAny(failureCode, "\x00\r\n")) {
		return DomainEvent{}, invalidValue("workItem.failureCode", "failure code is invalid or exceeds the bounded limit")
	}
	w.Status = statusForOutcome(outcome)
	w.FinishedAt = at.UTC()
	w.FailureCode = failureCode
	event := w.event(EventWorkItemSettled, at)
	event.Payload["outcome"] = string(outcome)
	if failureCode != "" {
		event.Payload["failureCode"] = failureCode
	}
	return event, nil
}

// Cancel removes a queued item before it can acquire a run or lease.
func (w *QueuedWorkItem) Cancel(at time.Time) (DomainEvent, error) {
	if w.Status != WorkItemQueued {
		return DomainEvent{}, invalidTransition("workItem", string(w.Status), string(WorkItemCancelled))
	}
	if at.Before(w.CreatedAt) {
		return DomainEvent{}, invalidValue("workItem.finishedAt", "cancellation time cannot precede creation")
	}
	w.Status = WorkItemCancelled
	w.FinishedAt = at.UTC()
	return w.event(EventWorkItemCancelled, at), nil
}

// Snapshot returns a defensive value copy of the work item.
func (w QueuedWorkItem) Snapshot() QueuedWorkItem {
	w.Execution = w.Execution.Snapshot()
	return w
}

func (w QueuedWorkItem) event(eventType DomainEventType, at time.Time) DomainEvent {
	event := newDomainEvent(eventType, at)
	event.TaskSession = w.TaskSessionID
	event.AgentThread = w.AgentThreadID
	event.AgentRun = w.AgentRunID
	event.WorkItem = w.ID
	event.Payload = map[string]string{
		"status":   string(w.Status),
		"sequence": strconv.FormatUint(w.Sequence, 10),
	}
	return event
}

func statusForOutcome(outcome AgentRunOutcome) WorkItemStatus {
	switch outcome {
	case RunCompleted:
		return WorkItemCompleted
	case RunPaused:
		return WorkItemPaused
	case RunFailed:
		return WorkItemFailed
	case RunInterrupted:
		return WorkItemInterrupted
	default:
		return WorkItemFailed
	}
}

func validWorkItemStatus(status WorkItemStatus) bool {
	switch status {
	case WorkItemQueued, WorkItemRunning, WorkItemPaused, WorkItemFailed, WorkItemInterrupted, WorkItemCompleted, WorkItemCancelled:
		return true
	default:
		return false
	}
}
