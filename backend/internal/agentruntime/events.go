package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"praxis/internal/core/domain"
)

const currentEntryVersion uint16 = 1

// SessionHeaderPayload records the approved inputs that establish a session boundary.
type SessionHeaderPayload struct {
	TaskSessionID     string `json:"taskSessionId"`
	AgentThreadID     string `json:"agentThreadId"`
	WorkspaceKey      string `json:"workspaceKey"`
	GrantID           string `json:"grantId"`
	TaskPacketID      string `json:"taskPacketId"`
	ContextManifestID string `json:"contextManifestId"`
	ModelID           string `json:"modelId"`
	SystemPromptHash  string `json:"systemPromptHash,omitempty"`
	ArtifactTplHash   string `json:"artifactTplHash,omitempty"`
}

type runStartedPayload struct {
	Reason string `json:"reason"`
}

type messagePayload struct {
	Role    MessageRole    `json:"role"`
	Content []ContentBlock `json:"content"`
}

type toolStartedPayload struct {
	ToolCallID  string            `json:"toolCallId"`
	Name        string            `json:"name"`
	Preflight   PreflightDecision `json:"preflight"`
	BlockReason string            `json:"blockReason,omitempty"`
}

type toolSettledPayload struct {
	ToolCallID string `json:"toolCallId"`
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"errorClass,omitempty"`
	SideEffect bool   `json:"sideEffect"`
}

type runSettledPayload struct {
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"errorClass,omitempty"`
	TurnCount  int    `json:"turnCount"`
}

func (r *Runtime) appendEntry(ctx context.Context, kind EntryKind, payload json.RawMessage, publish bool) error {
	r.entryMu.Lock()
	defer r.entryMu.Unlock()
	sequence := r.entrySequence + 1
	runID := r.currentRunID()
	if kind == EntrySessionHeader {
		runID = ""
	}
	entry := AgentSessionEntry{
		ID:       fmt.Sprintf("entry-%d", sequence),
		Sequence: sequence,
		At:       r.now(),
		Kind:     kind,
		Version:  currentEntryVersion,
		RunID:    runID,
		Payload:  cloneRaw(payload),
	}
	if err := r.config.SessionStore.Append(ctx, []AgentSessionEntry{entry}); err != nil {
		return &RuntimeError{Code: ErrorStorage, Message: "session entry could not be appended", Cause: err}
	}
	r.entrySequence = sequence
	if publish {
		r.publishEntry(entry)
	}
	return nil
}

func (r *Runtime) publishEntry(entry AgentSessionEntry) {
	kind := RuntimeEventKind(entry.Kind)
	runID := entry.RunID
	if runID == "" {
		runID = r.currentRunID()
	}
	payload := map[string]string{"entryId": entry.ID}
	if entry.Kind == EntryToolStarted || entry.Kind == EntryToolSettled || entry.Kind == EntryQueueEnqueued || entry.Kind == EntryQueueConsumed {
		var values map[string]any
		if json.Unmarshal(entry.Payload, &values) == nil {
			for key, value := range values {
				if text, ok := value.(string); ok {
					payload[key] = text
				}
			}
		}
	}
	r.publish(RuntimeEvent{TaskSessionID: r.config.TaskSessionID, AgentThreadID: r.config.AgentThreadID, AgentRunID: runID, Sequence: r.nextEventSequence(), Kind: kind, Payload: payload})
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
