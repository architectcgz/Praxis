package runtime

import (
	"context"

	"praxis/internal/core/domain"
)

// Settlement is the core-owned contract used by a runtime to hand a durable
// run outcome back to product orchestration.
type Settlement struct {
	RunID       domain.AgentRunID
	ThreadID    domain.AgentThreadID
	Outcome     domain.AgentRunOutcome
	FailureCode string
}

// SettlementHandler commits the product-side run, thread and queue state.
type SettlementHandler func(context.Context, Settlement) error

// RuntimeConfig is the credential-free, immutable input snapshot for one run.
type RuntimeConfig struct {
	Thread           domain.AgentThread
	TaskPacket       domain.TaskPacket
	ContextManifest  domain.ContextManifest
	Grant            domain.CapabilityGrant
	Execution        domain.RuntimeExecutionSnapshot
	Model            domain.ModelRef
	SessionReference string
	LeaseReference   string
	OnSettle         SettlementHandler
}

// RuntimeEvent is the runtime-owned event projection delivered after durable
// session writes. Product state transitions use Settlement instead.
type RuntimeEvent struct {
	TaskSessionID domain.TaskSessionID
	AgentThreadID domain.AgentThreadID
	AgentRunID    domain.AgentRunID
	Sequence      uint64
	Kind          string
	Payload       map[string]string
}

type Runtime interface {
	// StartExecution activates one already durable run and returns after the
	// asynchronous execution loop has been accepted by the runtime.
	StartExecution(ctx context.Context, run domain.AgentRun, prompt string) error
	RequestPause(ctx context.Context) error
	Events() <-chan RuntimeEvent
	Close(ctx context.Context) error
}

type RuntimeFactory interface {
	New(ctx context.Context, config RuntimeConfig) (Runtime, error)
}
