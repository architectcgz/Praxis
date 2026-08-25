package agentruntime

import (
	"context"
	"time"

	"praxis/internal/core/domain"
)

// SessionEventKind identifies a runtime timeline fact that must be made durable.
// It is independent from the JSONL entry kind owned by sessionlog.
type SessionEventKind string

const (
	SessionEventRunStarted  SessionEventKind = "run_started"
	SessionEventMessage     SessionEventKind = "message"
	SessionEventToolStarted SessionEventKind = "tool_started"
	SessionEventToolSettled SessionEventKind = "tool_settled"
	SessionEventQueueAdd    SessionEventKind = "queue_enqueued"
	SessionEventQueueTake   SessionEventKind = "queue_consumed"
	SessionEventRunSettled  SessionEventKind = "run_settled"
	SessionEventInterrupted SessionEventKind = "operation_interrupted"
)

// SessionEvent is a typed runtime fact. It is neither an append operation nor a
// physical log record; the session writer owns those concerns.
type SessionEvent struct {
	Kind       SessionEventKind
	RunID      domain.AgentRunID
	OccurredAt time.Time
	Payload    SessionEventPayload
}

// SessionEventPayload restricts a timeline fact to the supported semantic payloads.
type SessionEventPayload interface{ sessionEventPayload() }

// SessionAppendResult identifies the durable record created by a session writer.
type SessionAppendResult struct {
	EntryID  string
	Sequence uint64
}

// RunStartedEvent records why a durable run began and the immutable inputs it used.
type RunStartedEvent struct {
	Reason string
	Inputs RunInputs
}

// MessageEvent records a provider-neutral message that participates in future context.
type MessageEvent struct{ Message TurnMessage }

// ToolStartedEvent records an authorization decision after its triggering message is durable.
type ToolStartedEvent struct {
	ToolCallID  string
	Name        string
	Preflight   PreflightDecision
	BlockReason string
}

// ToolSettledEvent records the audit outcome of one tool invocation.
type ToolSettledEvent struct {
	ToolCallID string
	Outcome    string
	ErrorClass string
	Duration   time.Duration
	SideEffect bool
}

// QueueEnqueuedEvent preserves same-runtime input until the next safe point.
type QueueEnqueuedEvent struct {
	Queue   QueueKind
	ItemID  string
	Content []TurnContentBlock
}

// QueueConsumedEvent records that a queued item left its queue.
type QueueConsumedEvent struct {
	Queue  QueueKind
	ItemID string
	Reason ConsumeReason
}

// RunSettledEvent records the durable run outcome before product settlement begins.
type RunSettledEvent struct {
	Outcome    domain.AgentRunOutcome
	ErrorClass string
	TurnCount  int
}

// OperationInterruptedEvent records a known incomplete operation without retrying it.
type OperationInterruptedEvent struct {
	Operation string
	TargetID  string
	Note      string
}

func (RunStartedEvent) sessionEventPayload()           {}
func (MessageEvent) sessionEventPayload()              {}
func (ToolStartedEvent) sessionEventPayload()          {}
func (ToolSettledEvent) sessionEventPayload()          {}
func (QueueEnqueuedEvent) sessionEventPayload()        {}
func (QueueConsumedEvent) sessionEventPayload()        {}
func (RunSettledEvent) sessionEventPayload()           {}
func (OperationInterruptedEvent) sessionEventPayload() {}

func (r *Runtime) appendSessionEvent(
	ctx context.Context,
	kind SessionEventKind,
	payload SessionEventPayload,
	publish bool,
) (SessionAppendResult, error) {
	r.entryMu.Lock()
	defer r.entryMu.Unlock()
	runID := r.currentRunID()
	result, err := r.config.SessionStore.Append(ctx, SessionEvent{
		Kind:       kind,
		RunID:      runID,
		OccurredAt: r.now(),
		Payload:    payload,
	})
	if err != nil {
		return SessionAppendResult{}, &RuntimeError{
			Code:    ErrorStorage,
			Message: "session event could not be appended",
			Cause:   err,
		}
	}
	if publish {
		r.publishSessionEvent(kind, runID, payload, result)
	}
	return result, nil
}

func (r *Runtime) publishSessionEvent(
	kind SessionEventKind,
	runID domain.AgentRunID,
	payload SessionEventPayload,
	result SessionAppendResult,
) {
	values := map[string]string{"entryId": result.EntryID}
	switch typed := payload.(type) {
	case ToolStartedEvent:
		values["toolCallId"] = typed.ToolCallID
		values["name"] = typed.Name
		values["preflight"] = string(typed.Preflight)
	case ToolSettledEvent:
		values["toolCallId"] = typed.ToolCallID
		values["outcome"] = typed.Outcome
	case QueueEnqueuedEvent:
		values["itemId"] = typed.ItemID
		values["queue"] = string(typed.Queue)
	case QueueConsumedEvent:
		values["itemId"] = typed.ItemID
		values["queue"] = string(typed.Queue)
		values["reason"] = string(typed.Reason)
	}
	r.publish(RuntimeEvent{
		TaskSessionID: r.config.TaskSessionID,
		AgentThreadID: r.config.AgentThreadID,
		AgentRunID:    runID,
		Sequence:      r.nextEventSequence(),
		Kind:          RuntimeEventKind(kind),
		Payload:       values,
	})
}

func (r *Runtime) publish(event RuntimeEvent) {
	event = cloneRuntimeEvent(event)
	for _, observer := range r.config.Observers {
		safeObserve(observer, event)
	}
	select {
	case r.events <- event:
	case <-r.lifecycleDone:
	}
}

func safeObserve(observer RuntimeObserver, event RuntimeEvent) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(cloneRuntimeEvent(event))
}

func (r *Runtime) nextEventSequence() uint64 {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.eventSequence++
	return r.eventSequence
}

func (r *Runtime) currentRunID() domain.AgentRunID {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeRun == nil {
		return ""
	}
	return r.activeRun.ID
}

func (r *Runtime) now() time.Time {
	if r.config.Clock != nil {
		return r.config.Clock.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runtime) flush(ctx context.Context) error {
	if flusher, ok := r.config.SessionStore.(SessionFlusher); ok {
		if err := flusher.Flush(ctx); err != nil {
			return &RuntimeError{Code: ErrorStorage, Message: "session flush failed", Cause: err}
		}
	}
	return nil
}
