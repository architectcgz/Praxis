package persistence

import (
	"context"

	"praxis/internal/core/domain"
)

// The target repositories intentionally expose only indexed recovery queries.
// Recovery must not load every aggregate and infer product state in memory.
type SessionRepository interface {
	Get(ctx context.Context, id domain.SessionID) (domain.Session, error)
	Save(ctx context.Context, session domain.Session) error
}

// SessionListRepository exposes the indexed session catalog used by the app
// shell. It stays separate from the aggregate repository so recovery-focused
// test doubles do not need to implement a UI-only query.
type SessionListRepository interface {
	List(ctx context.Context, limit int) ([]domain.Session, error)
}

type AgentGroupRepository interface {
	Get(ctx context.Context, id domain.AgentGroupID) (domain.AgentGroup, error)
	Save(ctx context.Context, group domain.AgentGroup) error
	ListBySession(ctx context.Context, sessionID domain.SessionID, limit int) ([]domain.AgentGroup, error)
}

type AgentRepository interface {
	Get(ctx context.Context, id domain.AgentID) (domain.Agent, error)
	Save(ctx context.Context, agent domain.Agent) error
	ListByGroup(ctx context.Context, groupID domain.AgentGroupID, limit int) ([]domain.Agent, error)
	ListAll(ctx context.Context, limit int) ([]domain.Agent, error)
}

type AgentExecutionRepository interface {
	Get(ctx context.Context, id domain.AgentExecutionID) (domain.AgentExecution, error)
	Save(ctx context.Context, execution domain.AgentExecution) error
	FindByRequest(
		ctx context.Context,
		agentID domain.AgentID,
		requestID domain.RequestID,
	) (domain.AgentExecution, error)
	GetActiveByAgent(ctx context.Context, agentID domain.AgentID) (domain.AgentExecution, error)
	ListByAgent(ctx context.Context, agentID domain.AgentID, limit int) ([]domain.AgentExecution, error)
	CountActiveByGroup(ctx context.Context, groupID domain.AgentGroupID) (int, error)
	ListRecoverable(ctx context.Context, limit int) ([]domain.AgentExecution, error)
	ListStarting(ctx context.Context, limit int) ([]domain.AgentExecution, error)
}

// QueuedWorkRepository exposes only durable independent-task records. The
// AgentOrchestrator composes queue, execution, and Agent state in one
// transaction; runtime actors never call this port.
type QueuedWorkRepository interface {
	Get(ctx context.Context, id domain.WorkItemID) (domain.QueuedWork, error)
	Save(ctx context.Context, work domain.QueuedWork) error
	NextSequence(ctx context.Context, agentID domain.AgentID) (uint64, error)
	FindNextPendingByAgent(ctx context.Context, agentID domain.AgentID) (domain.QueuedWork, error)
	ListRunning(ctx context.Context, limit int) ([]domain.QueuedWork, error)
}

type WaitConditionRepository interface {
	Get(ctx context.Context, id domain.WaitConditionID) (domain.WaitCondition, error)
	Save(ctx context.Context, condition domain.WaitCondition) error
	ListUnresolvedByAgent(ctx context.Context, agentID domain.AgentID, limit int) ([]domain.WaitCondition, error)
}

type AgentControlRequestRepository interface {
	Get(ctx context.Context, id domain.AgentControlRequestID) (domain.AgentControlRequest, error)
	Save(ctx context.Context, request domain.AgentControlRequest) error
	ListOpenByAgent(ctx context.Context, agentID domain.AgentID, limit int) ([]domain.AgentControlRequest, error)
}

type ContextDeliveryRepository interface {
	Get(ctx context.Context, id domain.DeliveryID) (domain.ContextDelivery, error)
	Save(ctx context.Context, delivery domain.ContextDelivery) error
	ListPendingByTarget(ctx context.Context, agentID domain.AgentID, limit int) ([]domain.ContextDelivery, error)
	ListInFlight(ctx context.Context, limit int) ([]domain.ContextDelivery, error)
	HasDeliveringByTarget(ctx context.Context, agentID domain.AgentID) (bool, error)
}
