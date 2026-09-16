package persistence

import (
	"context"
	domaincontext "praxis/internal/domain/context"
	"time"

	domainagent "praxis/internal/domain/agent"
	domaincommand "praxis/internal/domain/command"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	domainsession "praxis/internal/domain/session"
	domainworkflow "praxis/internal/domain/workflow"
)

// The target repositories intentionally expose only indexed recovery queries.
// Recovery must not load every aggregate and infer product state in memory.
type SessionRepository interface {
	Get(ctx context.Context, id domainfoundation.SessionID) (domainsession.Session, error)
	Save(ctx context.Context, session domainsession.Session) error
}

// SessionListRepository exposes the indexed session catalog used by the app
// shell. It stays separate from the aggregate repository so recovery-focused
// test doubles do not need to implement a UI-only query.
type SessionListRepository interface {
	List(ctx context.Context, limit int) ([]domainsession.Session, error)
}

type ProjectSessionListRepository interface {
	ListByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainsession.Session, error)
}

type SessionContextRepository interface {
	CurrentRevision(context.Context, domainfoundation.SessionID) (uint64, error)
	Append(context.Context, domaincontext.SessionContextEntry, uint64) error
	List(context.Context, domainfoundation.SessionID, uint64, int) ([]domaincontext.SessionContextEntry, error)
}

type CommandReceiptRepository interface {
	Get(context.Context, domainfoundation.RequestID) (domaincommand.CommandReceipt, error)
	Save(context.Context, domaincommand.CommandReceipt) error
}

type AgentRepository interface {
	Get(ctx context.Context, id domainfoundation.AgentID) (domainagent.Agent, error)
	Save(ctx context.Context, agent domainagent.Agent) error
	ListBySession(ctx context.Context, sessionID domainfoundation.SessionID, limit int) ([]domainagent.Agent, error)
}

type AgentRecoveryRepository interface {
	ListAllAfter(context.Context, domainfoundation.SessionID, domainfoundation.AgentID, int) ([]domainagent.Agent, error)
}

type AgentExecutionRepository interface {
	Get(ctx context.Context, id domainfoundation.AgentExecutionID) (domainexecution.AgentExecution, error)
	Save(ctx context.Context, execution domainexecution.AgentExecution) error
	FindByRequest(
		ctx context.Context,
		agentID domainfoundation.AgentID,
		requestID domainfoundation.RequestID,
	) (domainexecution.AgentExecution, error)
	GetActiveByAgent(ctx context.Context, agentID domainfoundation.AgentID) (domainexecution.AgentExecution, error)
	ListByAgent(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]domainexecution.AgentExecution, error)
	CountActiveBySession(ctx context.Context, sessionID domainfoundation.SessionID) (int, error)
}

// ExecutionSecuritySnapshotRepository exposes the immutable authorization row
// for recovery and audit queries. Lifecycle updates remain on AgentExecution.
type ExecutionSecuritySnapshotRepository interface {
	Get(ctx context.Context, executionID domainfoundation.AgentExecutionID) (domainsecurity.ExecutionSecuritySnapshot, error)
}

// ToolInvocationRepository stores the durable identity and settlement of each
// provider tool call. Implementations must enforce execution-scoped call IDs.
type ToolInvocationRepository interface {
	Get(ctx context.Context, id domainfoundation.ToolInvocationID) (domainexecution.ToolInvocation, error)
	FindByExecutionCall(
		ctx context.Context,
		executionID domainfoundation.AgentExecutionID,
		providerToolCallID string,
	) (domainexecution.ToolInvocation, error)
	Save(ctx context.Context, invocation domainexecution.ToolInvocation) error
}

type ExecutionRecoveryRepository interface {
	ListRecoverableAfter(context.Context, time.Time, domainfoundation.AgentExecutionID, int) ([]domainexecution.AgentExecution, error)
	ListStartingAfter(context.Context, time.Time, domainfoundation.AgentExecutionID, int) ([]domainexecution.AgentExecution, error)
}

// QueuedWorkRepository exposes only durable independent-task records. The
// application services compose queue, execution, and Agent state in one
// transaction; runtime actors never call this interface.
type QueuedWorkRepository interface {
	Get(ctx context.Context, id domainfoundation.WorkItemID) (domainworkflow.QueuedWork, error)
	Save(ctx context.Context, work domainworkflow.QueuedWork) error
	NextSequence(ctx context.Context, agentID domainfoundation.AgentID) (uint64, error)
	FindNextPendingByAgent(ctx context.Context, agentID domainfoundation.AgentID) (domainworkflow.QueuedWork, error)
	ListRunning(ctx context.Context, limit int) ([]domainworkflow.QueuedWork, error)
}

type WaitConditionRepository interface {
	Get(ctx context.Context, id domainfoundation.WaitConditionID) (domainworkflow.WaitCondition, error)
	Save(ctx context.Context, condition domainworkflow.WaitCondition) error
	ListUnresolvedByAgent(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]domainworkflow.WaitCondition, error)
}

type WaitConditionRecoveryRepository interface {
	ListUnresolvedByAgentAfter(context.Context, domainfoundation.AgentID, domainfoundation.WaitConditionID, int) ([]domainworkflow.WaitCondition, error)
}

type AgentControlCommandRepository interface {
	Get(ctx context.Context, id domainfoundation.AgentControlCommandID) (domainworkflow.AgentControlCommand, error)
	Save(ctx context.Context, request domainworkflow.AgentControlCommand) error
	ListOpenByAgent(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]domainworkflow.AgentControlCommand, error)
}

type AgentControlRecoveryRepository interface {
	ListOpenByAgentAfter(context.Context, domainfoundation.AgentID, domainfoundation.AgentControlCommandID, int) ([]domainworkflow.AgentControlCommand, error)
}

type ContextDeliveryRepository interface {
	Get(ctx context.Context, id domainfoundation.DeliveryID) (domainworkflow.ContextDelivery, error)
	Save(ctx context.Context, delivery domainworkflow.ContextDelivery) error
	ListPendingByTarget(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]domainworkflow.ContextDelivery, error)
	ListInFlight(ctx context.Context, limit int) ([]domainworkflow.ContextDelivery, error)
	HasDeliveringByTarget(ctx context.Context, agentID domainfoundation.AgentID) (bool, error)
}

// ContextDeliveryRecoveryRepository provides cursor-based recovery scanning;
// UI page limits must never define recovery completeness.
type ContextDeliveryRecoveryRepository interface {
	ListInFlightAfter(context.Context, domainfoundation.DeliveryID, int) ([]domainworkflow.ContextDelivery, error)
}
