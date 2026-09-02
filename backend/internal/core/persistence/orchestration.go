package persistence

import (
	"context"
	domaincontext "praxis/internal/core/domain/context"
	"time"

	domainagent "praxis/internal/core/domain/agent"
	domaincommand "praxis/internal/core/domain/command"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
	domainsession "praxis/internal/core/domain/session"
	domainworkflow "praxis/internal/core/domain/workflow"
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

type ExecutionRecoveryRepository interface {
	ListRecoverableAfter(context.Context, time.Time, domainfoundation.AgentExecutionID, int) ([]domainexecution.AgentExecution, error)
	ListStartingAfter(context.Context, time.Time, domainfoundation.AgentExecutionID, int) ([]domainexecution.AgentExecution, error)
}

// QueuedWorkRepository exposes only durable independent-task records. The
// AgentOrchestrator composes queue, execution, and Agent state in one
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

type AgentControlRequestRepository interface {
	Get(ctx context.Context, id domainfoundation.AgentControlRequestID) (domainworkflow.AgentControlRequest, error)
	Save(ctx context.Context, request domainworkflow.AgentControlRequest) error
	ListOpenByAgent(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]domainworkflow.AgentControlRequest, error)
}

type AgentControlRecoveryRepository interface {
	ListOpenByAgentAfter(context.Context, domainfoundation.AgentID, domainfoundation.AgentControlRequestID, int) ([]domainworkflow.AgentControlRequest, error)
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
