package sessionlog

import (
	"encoding/json"
	"time"
)

const currentEntryVersion uint16 = 1

// SessionLogEntryKind identifies a record in the versioned JSONL wire format.
type SessionLogEntryKind string

const (
	SessionLogHeader      SessionLogEntryKind = "session_header"
	SessionLogRunStarted  SessionLogEntryKind = "run_started"
	SessionLogMessage     SessionLogEntryKind = "message"
	SessionLogToolStarted SessionLogEntryKind = "tool_started"
	SessionLogToolSettled SessionLogEntryKind = "tool_settled"
	SessionLogQueueAdd    SessionLogEntryKind = "queue_enqueued"
	SessionLogQueueTake   SessionLogEntryKind = "queue_consumed"
	SessionLogArtifact    SessionLogEntryKind = "context_artifact"
	SessionLogRunSettled  SessionLogEntryKind = "run_settled"
	SessionLogInterrupted SessionLogEntryKind = "operation_interrupted"
)

// SessionLogEntry is the durable JSONL envelope. It belongs exclusively to
// the sessionlog adapter so runtime and core do not depend on a file format.
type SessionLogEntry struct {
	ID       string              `json:"id"`
	Sequence uint64              `json:"seq"`
	At       time.Time           `json:"at"`
	Kind     SessionLogEntryKind `json:"kind"`
	Version  uint16              `json:"ver"`
	RunID    string              `json:"runId,omitempty"`
	Payload  json.RawMessage     `json:"payload"`
}

// SessionLogHeaderPayload establishes the immutable session identity and
// compatibility floor for an append-only session file.
type SessionLogHeaderPayload struct {
	TaskSessionID    string `json:"taskSessionId"`
	AgentThreadID    string `json:"agentThreadId"`
	Profile          string `json:"profile"`
	WorkspaceKey     string `json:"workspaceKey"`
	InjectionNonce   string `json:"injectionNonce"`
	MinReaderVersion uint16 `json:"minReaderVer"`
	WrittenBy        string `json:"writtenBy"`
}

// SessionLogModelRef is the persisted model identity used for run diagnostics.
type SessionLogModelRef struct {
	ID string `json:"id"`
}

// SessionLogMessagePayload is the only durable owner of normal context messages.
type SessionLogMessagePayload struct {
	Role       SessionLogMessageRole    `json:"role"`
	Content    []SessionLogContentBlock `json:"content"`
	StopReason string                   `json:"stopReason,omitempty"`
}

// SessionLogMessageRole identifies the participant that produced a stored message.
type SessionLogMessageRole string

const (
	SessionLogRoleUser      SessionLogMessageRole = "user"
	SessionLogRoleAssistant SessionLogMessageRole = "assistant"
)

// SessionLogContentBlockKind identifies the storage representation of context content.
type SessionLogContentBlockKind string

const (
	SessionLogContentText       SessionLogContentBlockKind = "text"
	SessionLogContentThinking   SessionLogContentBlockKind = "thinking"
	SessionLogContentToolUse    SessionLogContentBlockKind = "tool_use"
	SessionLogContentToolResult SessionLogContentBlockKind = "tool_result"
)

// SessionLogContentBlock preserves provider replay material, including unknown
// blocks, without making those wire details runtime-owned types.
type SessionLogContentBlock struct {
	Kind       SessionLogContentBlockKind `json:"kind"`
	Text       string                     `json:"text,omitempty"`
	Signature  string                     `json:"signature,omitempty"`
	ToolUse    *SessionLogToolUseBlock    `json:"toolUse,omitempty"`
	ToolResult *SessionLogToolResultBlock `json:"toolResult,omitempty"`
	Raw        json.RawMessage            `json:"raw,omitempty"`
}

// UnmarshalJSON preserves an unrecognized block byte-for-byte for transcript
// inspection while the reader excludes it from a provider context projection.
func (contentBlock *SessionLogContentBlock) UnmarshalJSON(jsonData []byte) error {
	type contentBlockWire SessionLogContentBlock
	var wire contentBlockWire
	if err := json.Unmarshal(jsonData, &wire); err != nil {
		return err
	}
	*contentBlock = SessionLogContentBlock(wire)
	if !isKnownContentBlockKind(contentBlock.Kind) {
		contentBlock.Raw = append(json.RawMessage(nil), jsonData...)
	}
	return nil
}

// MarshalJSON writes unknown content from its preserved wire representation.
func (contentBlock SessionLogContentBlock) MarshalJSON() ([]byte, error) {
	if !isKnownContentBlockKind(contentBlock.Kind) && len(contentBlock.Raw) > 0 {
		return append([]byte(nil), contentBlock.Raw...), nil
	}
	type contentBlockWire SessionLogContentBlock
	return json.Marshal(contentBlockWire(contentBlock))
}

func isKnownContentBlockKind(kind SessionLogContentBlockKind) bool {
	switch kind {
	case SessionLogContentText, SessionLogContentThinking, SessionLogContentToolUse, SessionLogContentToolResult:
		return true
	default:
		return false
	}
}

// SessionLogToolUseBlock stores one assistant-requested tool invocation.
type SessionLogToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// SessionLogToolResultBlock stores the model-visible result of a tool invocation.
type SessionLogToolResultBlock struct {
	ToolCallID string                   `json:"toolCallId"`
	Content    []SessionLogContentBlock `json:"content,omitempty"`
	BlobRef    json.RawMessage          `json:"blobRef,omitempty"`
	IsError    bool                     `json:"isError,omitempty"`
}

// SessionLogRunStartedPayload records a run's reason and immutable input snapshot.
type SessionLogRunStartedPayload struct {
	Reason               string                      `json:"reason"`
	TaskPacketID         string                      `json:"taskPacketId"`
	ContextManifestID    string                      `json:"contextManifestId"`
	GrantID              string                      `json:"grantId"`
	Execution            SessionLogExecutionSnapshot `json:"execution"`
	ModelRef             SessionLogModelRef          `json:"modelRef"`
	SystemPromptHash     string                      `json:"systemPromptHash"`
	ToolDefHashes        map[string]string           `json:"toolDefHashes"`
	ArtifactTemplateHash string                      `json:"artifactTplHash"`
}

// SessionLogExecutionSnapshot records the execution modes that governed a run.
type SessionLogExecutionSnapshot struct {
	SandboxMode  string `json:"sandboxMode"`
	ApprovalMode string `json:"approvalMode"`
}

// SessionLogToolStartedPayload records the preflight decision for one tool call.
type SessionLogToolStartedPayload struct {
	ToolCallID  string `json:"toolCallId"`
	Name        string `json:"name"`
	Preflight   string `json:"preflight"`
	BlockReason string `json:"blockReason,omitempty"`
}

// SessionLogToolSettledPayload records the terminal result of one tool call.
type SessionLogToolSettledPayload struct {
	ToolCallID string `json:"toolCallId"`
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"errorClass,omitempty"`
	DurationMS int64  `json:"durationMs"`
	SideEffect bool   `json:"sideEffect"`
}

// SessionLogQueueEnqueuedPayload retains queue content until it is consumed.
type SessionLogQueueEnqueuedPayload struct {
	Queue   string                   `json:"queue"`
	ItemID  string                   `json:"itemId"`
	Content []SessionLogContentBlock `json:"content"`
}

// SessionLogQueueConsumedPayload records why a queued item left its queue.
type SessionLogQueueConsumedPayload struct {
	Queue  string `json:"queue"`
	ItemID string `json:"itemId"`
	Reason string `json:"reason"`
}

// SessionLogContextArtifactPayload stores an approved cross-boundary artifact.
type SessionLogContextArtifactPayload struct {
	ArtifactKind   string          `json:"artifactKind"`
	ArtifactID     string          `json:"artifactId,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Body           json.RawMessage `json:"body"`
}

// SessionLogRunSettledPayload records the durable outcome of a run.
type SessionLogRunSettledPayload struct {
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"errorClass,omitempty"`
	TurnCount  int    `json:"turnCount"`
}

// SessionLogOperationInterruptedPayload records an operation that recovery must not replay.
type SessionLogOperationInterruptedPayload struct {
	Operation string `json:"operation"`
	TargetID  string `json:"targetId"`
	Note      string `json:"note"`
}
