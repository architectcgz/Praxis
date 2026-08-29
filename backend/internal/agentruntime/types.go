package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"praxis/internal/core/domain"
)

// AgentOutputEventKind identifies a non-durable provider output notification.
// These events are for live presentation only; the transcript remains the
// recovery source for complete messages.
type AgentOutputEventKind string

const (
	AgentOutputTextDelta AgentOutputEventKind = "text_delta"
	AgentOutputSettled   AgentOutputEventKind = "settled"
)

// AgentOutputEvent is a transient projection of output from one execution.
// Text is set only for text_delta events; settled tells subscribers to reload
// the durable transcript and execution state.
type AgentOutputEvent struct {
	Kind        AgentOutputEventKind    `json:"kind"`
	AgentID     domain.AgentID          `json:"agentId"`
	ExecutionID domain.AgentExecutionID `json:"executionId"`
	Text        string                  `json:"text"`
}

// AgentOutputObserver receives non-durable execution output. Implementations
// must return promptly so provider stream consumption is not delayed.
type AgentOutputObserver func(AgentOutputEvent)

// SessionContext is the safe provider-facing projection of a session log.
// Raw records remain inside the transcript adapter.
type SessionContext struct {
	HasManifest bool
	CanContinue bool
	Messages    []TurnMessage
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
	RuntimeEventRunStarted      RuntimeEventKind = "run_started"
	RuntimeEventMessage         RuntimeEventKind = "message"
	RuntimeEventToolStarted     RuntimeEventKind = "tool_started"
	RuntimeEventToolSettled     RuntimeEventKind = "tool_settled"
	RuntimeEventQueueEnqueued   RuntimeEventKind = "queue_enqueued"
	RuntimeEventQueueConsumed   RuntimeEventKind = "queue_consumed"
	RuntimeEventContextArtifact RuntimeEventKind = "context_artifact"
	RuntimeEventRunSettled      RuntimeEventKind = "run_settled"
	RuntimeEventInterrupted     RuntimeEventKind = "operation_interrupted"
)

const (
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

// RuntimeEvent is the ordered UI/control-plane projection of a durable session event.
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
	// ToolDefinitionHashes are recorded with each run_started event; reader
	// compatibility metadata remains in the session manifest.
	ToolDefinitionHashes map[string]string
	MinimumReaderVersion uint16
	WrittenBy            string
	ModelStream          ModelStreamPort
	ToolExecutor         ToolExecutor
	SessionStore         AgentSessionStore
	Approval             CommandApproval
	Critical             []SettlementListener
	Observers            []RuntimeObserver
	Clock                Clock
	EventBuffer          int
	LeaseReference       string
	InjectionNonce       string
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
	if c.ModelStream == nil || c.SessionStore == nil {
		return fmt.Errorf("runtime config: model and session ports are required")
	}
	if len(c.Grant.AllowedTools) > 0 && c.ToolExecutor == nil {
		return fmt.Errorf("runtime config: tool executor is required when tools are granted")
	}
	if c.EventBuffer < 0 {
		return fmt.Errorf("runtime config: event buffer cannot be negative")
	}
	return nil
}
