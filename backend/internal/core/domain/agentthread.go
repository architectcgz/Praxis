package domain

import "time"

type AgentThreadState string

const (
	ThreadIdle        AgentThreadState = "idle"
	ThreadRunning     AgentThreadState = "running"
	ThreadPausing     AgentThreadState = "pausing"
	ThreadPaused      AgentThreadState = "paused"
	ThreadInterrupted AgentThreadState = "interrupted"
	ThreadFailed      AgentThreadState = "failed"
	ThreadClosed      AgentThreadState = "closed"
)

type AgentThread struct {
	ID                AgentThreadID
	TaskSessionID     TaskSessionID
	Profile           AgentProfile
	TaskPacketID      TaskPacketID
	ContextManifestID ContextManifestID
	GrantID           CapabilityGrantID
	RuntimeSessionRef string
	State             AgentThreadState
	CurrentRunID      AgentRunID
	UpdatedAt         time.Time
}

func NewAgentThread(
	id AgentThreadID,
	sessionID TaskSessionID,
	profile AgentProfile,
	packetID TaskPacketID,
	manifestID ContextManifestID,
	grantID CapabilityGrantID,
	at time.Time,
) (AgentThread, error) {
	thread := AgentThread{
		ID:                id,
		TaskSessionID:     sessionID,
		Profile:           profile,
		TaskPacketID:      packetID,
		ContextManifestID: manifestID,
		GrantID:           grantID,
		State:             ThreadIdle,
		UpdatedAt:         at.UTC(),
	}
	if err := thread.Validate(); err != nil {
		return AgentThread{}, err
	}
	return thread, nil
}

func (t AgentThread) Validate() error {
	if idIsEmpty(string(t.ID)) || idIsEmpty(string(t.TaskSessionID)) || idIsEmpty(string(t.TaskPacketID)) ||
		idIsEmpty(string(t.ContextManifestID)) ||
		idIsEmpty(string(t.GrantID)) {
		return invalidValue("agentThread", "required reference is missing")
	}
	if !t.Profile.Valid() {
		return invalidValue("agentThread.profile", "unknown agent profile")
	}
	if !validThreadState(t.State) {
		return invalidValue("agentThread.state", "unknown thread state")
	}
	if t.UpdatedAt.IsZero() {
		return invalidValue("agentThread.updatedAt", "update time is required")
	}
	return nil
}

func (t *AgentThread) Start(runID AgentRunID, at time.Time) (DomainEvent, error) {
	if t.State != ThreadIdle && t.State != ThreadPaused && t.State != ThreadInterrupted && t.State != ThreadFailed {
		return DomainEvent{}, invalidTransition("agentThread", string(t.State), string(ThreadRunning))
	}
	if idIsEmpty(string(runID)) {
		return DomainEvent{}, invalidValue("agentThread.currentRunID", "run id is required")
	}
	t.State, t.CurrentRunID, t.UpdatedAt = ThreadRunning, runID, at.UTC()
	event := newDomainEvent(EventThreadStarted, at)
	event.TaskSession, event.AgentThread, event.AgentRun = t.TaskSessionID, t.ID, runID
	return event, nil
}

func (t *AgentThread) RequestPause(at time.Time) (DomainEvent, error) {
	if t.State != ThreadRunning {
		return DomainEvent{}, invalidTransition("agentThread", string(t.State), string(ThreadPausing))
	}
	t.State, t.UpdatedAt = ThreadPausing, at.UTC()
	event := newDomainEvent(EventThreadPausing, at)
	event.TaskSession, event.AgentThread, event.AgentRun = t.TaskSessionID, t.ID, t.CurrentRunID
	return event, nil
}

func (t *AgentThread) Pause(at time.Time) (DomainEvent, error) {
	if t.State != ThreadPausing {
		return DomainEvent{}, invalidTransition("agentThread", string(t.State), string(ThreadPaused))
	}
	t.State, t.UpdatedAt = ThreadPaused, at.UTC()
	event := newDomainEvent(EventThreadPaused, at)
	event.TaskSession, event.AgentThread, event.AgentRun = t.TaskSessionID, t.ID, t.CurrentRunID
	return event, nil
}

// Settle applies a completed AgentRun outcome to the owning thread and emits
// the durable lifecycle event for the resulting thread state. A paused run
// must first pass through ThreadPausing so pause requests cannot be reported
// as settled without the control-plane acknowledgement.
func (t *AgentThread) Settle(outcome AgentRunOutcome, at time.Time) (DomainEvent, error) {
	if t.State != ThreadRunning && t.State != ThreadPausing {
		return DomainEvent{}, invalidTransition("agentThread", string(t.State), "settled")
	}
	if !validRunOutcome(outcome) {
		return DomainEvent{}, invalidValue("agentThread.outcome", "unknown run outcome")
	}
	if outcome == RunPaused && t.State != ThreadPausing {
		return DomainEvent{}, invalidTransition("agentThread", string(t.State), string(ThreadPaused))
	}
	// A completed run makes the thread eligible for the next queued work item;
	// non-completed outcomes deliberately stop automatic queue progression.
	switch outcome {
	case RunPaused:
		t.State = ThreadPaused
	case RunFailed:
		t.State = ThreadFailed
	case RunInterrupted:
		t.State = ThreadInterrupted
	default:
		t.State = ThreadIdle
	}
	t.UpdatedAt = at.UTC()
	event := newDomainEvent(EventThreadSettled, at)
	event.TaskSession, event.AgentThread, event.AgentRun = t.TaskSessionID, t.ID, t.CurrentRunID
	event.Payload = map[string]string{"outcome": string(outcome)}
	return event, nil
}

func (t *AgentThread) Close(at time.Time) (DomainEvent, error) {
	if t.State != ThreadIdle && t.State != ThreadPaused && t.State != ThreadInterrupted && t.State != ThreadFailed {
		return DomainEvent{}, invalidTransition("agentThread", string(t.State), string(ThreadClosed))
	}
	t.State, t.UpdatedAt = ThreadClosed, at.UTC()
	event := newDomainEvent(EventThreadClosed, at)
	event.TaskSession, event.AgentThread = t.TaskSessionID, t.ID
	return event, nil
}

func validThreadState(state AgentThreadState) bool {
	switch state {
	case ThreadIdle, ThreadRunning, ThreadPausing, ThreadPaused, ThreadInterrupted, ThreadFailed, ThreadClosed:
		return true
	default:
		return false
	}
}
