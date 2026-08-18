package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"praxis/internal/core/domain"
)

// EntryKind identifies the durable lifecycle and transcript records written by a runtime.
type EntryKind string

const (
	EntrySessionHeader        EntryKind = "session_header"
	EntryRunStarted           EntryKind = "run_started"
	EntryMessage              EntryKind = "message"
	EntryToolStarted          EntryKind = "tool_started"
	EntryToolSettled          EntryKind = "tool_settled"
	EntryQueueEnqueued        EntryKind = "queue_enqueued"
	EntryQueueConsumed        EntryKind = "queue_consumed"
	EntryContextArtifact      EntryKind = "context_artifact"
	EntryRunSettled           EntryKind = "run_settled"
	EntryOperationInterrupted EntryKind = "operation_interrupted"
)

// MessageRole is the role of a durable agent message.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// ContentBlockKind identifies a block within a message.
type ContentBlockKind string

const (
	ContentText       ContentBlockKind = "text"
	ContentToolUse    ContentBlockKind = "tool_use"
	ContentToolResult ContentBlockKind = "tool_result"
)

// ContentBlock is the provider-neutral representation of message content.
type ContentBlock struct {
	Kind       ContentBlockKind
	Text       string
	ToolCallID string
	ToolName   string
	Input      json.RawMessage
	IsError    bool
}

// Message is a complete user or assistant message used to build a turn snapshot.
type Message struct {
	Role    MessageRole
	Content []ContentBlock
}

// ToolDefinition describes a tool exposed to the model after Grant filtering.
type ToolDefinition struct {
	Name        domain.ToolName
	Description string
	InputSchema json.RawMessage
}

// ToolCall is a provider-neutral request to execute one tool.
type ToolCall struct {
	ID                  string
	Name                domain.ToolName
	Input               json.RawMessage
	Arguments           json.RawMessage
	Path                string
	WorkingDirectory    string
	RequiresWrite       bool
	RequiresNetwork     bool
	CommandConfirmation string
}

// ToolExecutionContext is the immutable authorization snapshot passed to a tool executor.
type ToolExecutionContext struct {
	Grant          domain.CapabilityGrant
	Execution      domain.RuntimeExecutionSnapshot
	LeaseReference string
}

// ToolExecutionResult is the bounded result returned by a tool executor.
type ToolExecutionResult struct {
	Content    string
	Output     string
	ErrorClass string
	SideEffect bool
}

// ModelStreamEventKind identifies an event emitted by a model provider adapter.
type ModelStreamEventKind string

const (
	StreamTextDelta ModelStreamEventKind = "text_delta"
	StreamToolCall  ModelStreamEventKind = "tool_call"
	StreamComplete  ModelStreamEventKind = "complete"
	StreamError     ModelStreamEventKind = "error"
)

// ModelStreamEvent is a provider-neutral event from one model request.
type ModelStreamEvent struct {
	Kind       ModelStreamEventKind
	Text       string
	ToolCall   ToolCall
	StopReason string
	Err        error
}

// ModelRequest is the request sent to a model provider. It contains no credential fields.
type ModelRequest struct {
	Snapshot TurnSnapshot
}

// TurnSnapshot is the immutable input for one model request.
type TurnSnapshot struct {
	RunID                domain.AgentRunID
	SessionReference     string
	Messages             []Message
	TaskPacket           domain.TaskPacket
	ContextManifest      domain.ContextManifest
	SystemPrompt         string
	SystemPromptHash     string
	ArtifactTemplateHash string
	Model                domain.ModelRef
	Tools                []ToolDefinition
	GrantID              domain.CapabilityGrantID
	Execution            domain.RuntimeExecutionSnapshot
	TurnNumber           int
}

// AgentSessionEntry is one append-only record in an agent session.
type AgentSessionEntry struct {
	ID       string
	Sequence uint64
	At       time.Time
	Kind     EntryKind
	Version  uint16
	RunID    domain.AgentRunID
	Payload  json.RawMessage
}

// AgentSessionContext is the safe context projection returned by an AgentSessionStore.
type AgentSessionContext struct {
	LastSequence uint64
	Messages     []Message
	Entries      []AgentSessionEntry
}

// SessionEntryRef identifies an existing durable artifact for idempotency checks.
type SessionEntryRef struct {
	Sequence uint64
	ID       string
}

// PromptConfig contains safe prompt metadata and prompt text, but no credentials.
type PromptConfig struct {
	SystemPrompt         string
	SystemPromptHash     string
	ArtifactTemplateHash string
}

// Clock supplies time to the runtime and can be replaced by a deterministic test clock.
type Clock interface {
	Now() time.Time
}

// RuntimeObserver receives best-effort runtime events. Observer failures never change control flow.
type RuntimeObserver func(RuntimeEvent)

// SettlementListener runs before the product settlement callback and may block settlement on failure.
type SettlementListener func(context.Context, Settlement) error

// SettlementHandler commits the product-side AgentRun and AgentThread outcome.
type SettlementHandler func(context.Context, Settlement) error

// Settlement is the outcome passed to product orchestration after runtime work is durable.
type Settlement struct {
	RunID       domain.AgentRunID
	ThreadID    domain.AgentThreadID
	Outcome     domain.AgentRunOutcome
	FailureCode string
	TurnCount   int
}

// QueueKind identifies one same-agent input queue.
type QueueKind string

const (
	QueueSteer    QueueKind = "steer"
	QueueFollowUp QueueKind = "follow_up"
	QueueNextTurn QueueKind = "next_turn"
)

// QueueItem is a durable same-agent user input.
type QueueItem struct {
	ID      string
	Queue   QueueKind
	Content string
}

// ConsumeReason explains why a queued item left its queue.
type ConsumeReason string

const (
	ConsumeDrained      ConsumeReason = "drained"
	ConsumeClearedAbort ConsumeReason = "cleared_by_abort"
)

// PreflightDecision is the result of the runtime authorization gate.
type PreflightDecision string

const (
	PreflightAllowed PreflightDecision = "allowed"
	PreflightBlocked PreflightDecision = "blocked"
)

// RuntimeEventKind identifies a published runtime event.
type RuntimeEventKind string

const (
	RuntimeEventSessionHeader   RuntimeEventKind = RuntimeEventKind(EntrySessionHeader)
	RuntimeEventRunStarted      RuntimeEventKind = RuntimeEventKind(EntryRunStarted)
	RuntimeEventMessage         RuntimeEventKind = RuntimeEventKind(EntryMessage)
	RuntimeEventToolStarted     RuntimeEventKind = RuntimeEventKind(EntryToolStarted)
	RuntimeEventToolSettled     RuntimeEventKind = RuntimeEventKind(EntryToolSettled)
	RuntimeEventQueueEnqueued   RuntimeEventKind = RuntimeEventKind(EntryQueueEnqueued)
	RuntimeEventQueueConsumed   RuntimeEventKind = RuntimeEventKind(EntryQueueConsumed)
	RuntimeEventContextArtifact RuntimeEventKind = RuntimeEventKind(EntryContextArtifact)
	RuntimeEventRunSettled      RuntimeEventKind = RuntimeEventKind(EntryRunSettled)
	RuntimeEventInterrupted     RuntimeEventKind = RuntimeEventKind(EntryOperationInterrupted)
)

const (
	EventSessionHeader   = RuntimeEventSessionHeader
	EventRunStarted      = RuntimeEventRunStarted
	EventMessage         = RuntimeEventMessage
	EventToolStarted     = RuntimeEventToolStarted
	EventToolSettled     = RuntimeEventToolSettled
	EventQueueEnqueued   = RuntimeEventQueueEnqueued
	EventQueueConsumed   = RuntimeEventQueueConsumed
	EventContextArtifact = RuntimeEventContextArtifact
	EventRunSettled      = RuntimeEventRunSettled
	EventInterrupted     = RuntimeEventInterrupted
)

// RuntimeEvent is the ordered UI/control-plane projection of a durable runtime entry.
type RuntimeEvent struct {
	TaskSessionID domain.TaskSessionID
	AgentThreadID domain.AgentThreadID
	AgentRunID    domain.AgentRunID
	Sequence      uint64
	Kind          RuntimeEventKind
	Payload       map[string]string
}

// CommandApproval asks the host to confirm a command when the execution mode requires it.
type CommandApproval interface {
	ApproveCommand(context.Context, ToolCall, ToolExecutionContext) (bool, error)
}

// RuntimeConfig is the fully materialized, credential-free input to one agent runtime.
type RuntimeConfig struct {
	TaskSessionID    domain.TaskSessionID
	AgentThreadID    domain.AgentThreadID
	Thread           domain.AgentThread
	TaskPacket       domain.TaskPacket
	ContextManifest  domain.ContextManifest
	Grant            domain.CapabilityGrant
	Execution        domain.RuntimeExecutionSnapshot
	Model            domain.ModelRef
	SessionReference string
	Prompt           PromptConfig

	ToolDefinitions []ToolDefinition
	ModelStream     ModelStreamPort
	ToolExecutor    ToolExecutor
	SessionStore    AgentSessionStore
	Approval        CommandApproval
	OnSettle        SettlementHandler
	Critical        []SettlementListener
	Observers       []RuntimeObserver
	Clock           Clock
	EventBuffer     int
	LeaseReference  string
}

func (c RuntimeConfig) validate() error {
	if c.TaskSessionID == "" && c.Thread.TaskSessionID != "" {
		c.TaskSessionID = c.Thread.TaskSessionID
	}
	if c.AgentThreadID == "" && c.Thread.ID != "" {
		c.AgentThreadID = c.Thread.ID
	}
	if c.Thread.TaskSessionID != "" && c.Thread.TaskSessionID != c.TaskSessionID {
		return fmt.Errorf("runtime config: thread task session does not match runtime identity")
	}
	if c.Thread.ID != "" && c.Thread.ID != c.AgentThreadID {
		return fmt.Errorf("runtime config: thread id does not match runtime identity")
	}
	if c.TaskSessionID == "" || c.AgentThreadID == "" {
		return fmt.Errorf("runtime config: task session and agent thread are required")
	}
	if err := c.TaskPacket.Validate(); err != nil {
		return fmt.Errorf("runtime config task packet: %w", err)
	}
	if err := c.ContextManifest.Validate(); err != nil {
		return fmt.Errorf("runtime config context manifest: %w", err)
	}
	if err := c.Grant.Validate(); err != nil {
		return fmt.Errorf("runtime config grant: %w", err)
	}
	if err := c.Execution.Validate(); err != nil {
		return fmt.Errorf("runtime config execution: %w", err)
	}
	if strings.TrimSpace(c.SessionReference) == "" {
		return fmt.Errorf("runtime config: session reference is required")
	}
	if c.Model.ID == "" {
		return fmt.Errorf("runtime config: model reference is required")
	}
	if c.ModelStream == nil || c.ToolExecutor == nil || c.SessionStore == nil {
		return fmt.Errorf("runtime config: model, tool and session ports are required")
	}
	if c.EventBuffer < 0 {
		return fmt.Errorf("runtime config: event buffer cannot be negative")
	}
	return nil
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneBlock(block ContentBlock) ContentBlock {
	block.Input = cloneRaw(block.Input)
	return block
}

func cloneMessage(message Message) Message {
	copy := message
	copy.Content = make([]ContentBlock, len(message.Content))
	for i, block := range message.Content {
		copy.Content[i] = cloneBlock(block)
	}
	return copy
}

func cloneMessages(messages []Message) []Message {
	result := make([]Message, len(messages))
	for i, message := range messages {
		result[i] = cloneMessage(message)
	}
	return result
}

func cloneToolCall(call ToolCall) ToolCall {
	if len(call.Input) == 0 && len(call.Arguments) > 0 {
		call.Input = call.Arguments
	}
	if len(call.Arguments) == 0 && len(call.Input) > 0 {
		call.Arguments = call.Input
	}
	call.Input = cloneRaw(call.Input)
	call.Arguments = cloneRaw(call.Arguments)
	return call
}

func cloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	result := make([]ToolDefinition, len(definitions))
	for i, definition := range definitions {
		result[i] = definition
		result[i].InputSchema = cloneRaw(definition.InputSchema)
	}
	return result
}

func cloneEntry(entry AgentSessionEntry) AgentSessionEntry {
	entry.Payload = cloneRaw(entry.Payload)
	return entry
}

func cloneEntries(entries []AgentSessionEntry) []AgentSessionEntry {
	result := make([]AgentSessionEntry, len(entries))
	for i, entry := range entries {
		result[i] = cloneEntry(entry)
	}
	return result
}

func cloneRuntimeEvent(event RuntimeEvent) RuntimeEvent {
	event.Payload = cloneStringMap(event.Payload)
	return event
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
