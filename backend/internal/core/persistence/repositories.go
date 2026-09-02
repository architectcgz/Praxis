package persistence

import (
	"context"
	"time"

	domainfoundation "praxis/internal/core/domain/foundation"
	domainproject "praxis/internal/core/domain/project"
	domainsecurity "praxis/internal/core/domain/security"
	domainworkflow "praxis/internal/core/domain/workflow"
	domainworkspace "praxis/internal/core/domain/workspace"
)

// ProjectRepository stores the durable project aggregate.
type ProjectRepository interface {
	Get(context.Context, domainfoundation.ProjectID) (domainproject.Project, error)
	Save(context.Context, domainproject.Project) error
}

// ProjectListRepository provides the indexed project catalog used by the UI.
type ProjectListRepository interface {
	List(context.Context, int) ([]domainproject.Project, error)
}

// WorkspaceRepository stores a project's execution workspace aggregate.
type WorkspaceRepository interface {
	Get(context.Context, domainfoundation.WorkspaceID) (domainworkspace.Workspace, error)
	Save(context.Context, domainworkspace.Workspace) error
	ListByProject(context.Context, domainfoundation.ProjectID, int) ([]domainworkspace.Workspace, error)
}

type DelegationRepository interface {
	Get(context.Context, domainfoundation.DelegationRequestID) (domainworkflow.DelegationRequest, error)
	Save(context.Context, domainworkflow.DelegationRequest) error
}

type WorkspaceLeaseRepository interface {
	GetActiveByWorkspace(context.Context, domainfoundation.WorkspaceID) (domainworkspace.WorkspaceWriteLease, error)
	Save(context.Context, domainworkspace.WorkspaceWriteLease) error
	Release(context.Context, domainworkspace.WorkspaceWriteLease) error
}

type AgentResultRepository interface {
	Get(context.Context, domainfoundation.AgentResultID) (domainworkflow.AgentResult, error)
	Save(context.Context, domainworkflow.AgentResult) error
}

type BriefingRepository interface {
	Get(context.Context, domainfoundation.BriefingID) (domainworkflow.Briefing, error)
	Save(context.Context, domainworkflow.Briefing) error
}

type NoteRepository interface {
	Get(context.Context, domainfoundation.NoteID) (domainworkflow.Note, error)
	Save(context.Context, domainworkflow.Note) error
}

type EventRepository interface {
	Append(context.Context, domainfoundation.DomainEvent) error
}

type EventQueryRepository interface {
	ListBySession(context.Context, domainfoundation.SessionID, time.Time, int) ([]domainfoundation.DomainEvent, error)
	ListByAgent(context.Context, domainfoundation.AgentID, time.Time, int) ([]domainfoundation.DomainEvent, error)
	ListByExecution(context.Context, domainfoundation.AgentExecutionID, time.Time, int) ([]domainfoundation.DomainEvent, error)
}

type AgentPolicyStore interface {
	Current(context.Context) (domainsecurity.AgentPolicySnapshot, error)
	Update(context.Context, domainsecurity.AgentPolicySnapshot) error
}

type TxRunner interface {
	InTx(context.Context, func(context.Context) error) error
}
