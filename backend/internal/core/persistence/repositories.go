package persistence

import (
	"context"

	"praxis/internal/core/domain"
)

// ProjectRepository stores the durable project aggregate.
type ProjectRepository interface {
	Get(context.Context, domain.ProjectID) (domain.Project, error)
	Save(context.Context, domain.Project) error
}

// ProjectListRepository provides the indexed project catalog used by the UI.
type ProjectListRepository interface {
	List(context.Context, int) ([]domain.Project, error)
}

// WorkspaceRepository stores a project's execution workspace aggregate.
type WorkspaceRepository interface {
	Get(context.Context, domain.WorkspaceID) (domain.Workspace, error)
	Save(context.Context, domain.Workspace) error
	ListByProject(context.Context, domain.ProjectID, int) ([]domain.Workspace, error)
}

type TaskPacketRepository interface {
	Get(context.Context, domain.TaskPacketID) (domain.TaskPacket, error)
	Save(context.Context, domain.TaskPacket) error
}

type ContextManifestRepository interface {
	Get(context.Context, domain.ContextManifestID) (domain.ContextManifest, error)
	Save(context.Context, domain.ContextManifest) error
}

type CapabilityGrantRepository interface {
	Get(context.Context, domain.CapabilityGrantID) (domain.CapabilityGrant, error)
	Save(context.Context, domain.CapabilityGrant) error
}

type DelegationRepository interface {
	Get(context.Context, domain.DelegationRequestID) (domain.DelegationRequest, error)
	Save(context.Context, domain.DelegationRequest) error
}

type WorkspaceLeaseRepository interface {
	GetActiveByWorkspace(context.Context, domain.WorkspaceID) (domain.WorkspaceWriteLease, error)
	Save(context.Context, domain.WorkspaceWriteLease) error
	Release(context.Context, domain.WorkspaceWriteLease) error
}

type AgentResultRepository interface {
	Get(context.Context, domain.AgentResultID) (domain.AgentResult, error)
	Save(context.Context, domain.AgentResult) error
}

type BriefingRepository interface {
	Get(context.Context, domain.BriefingID) (domain.Briefing, error)
	Save(context.Context, domain.Briefing) error
}

type NoteRepository interface {
	Get(context.Context, domain.NoteID) (domain.Note, error)
	Save(context.Context, domain.Note) error
}

type EventRepository interface {
	Append(context.Context, domain.DomainEvent) error
}

type AgentPolicyStore interface {
	Current(context.Context) (domain.AgentPolicySnapshot, error)
	Update(context.Context, domain.AgentPolicySnapshot) error
}

type TxRunner interface {
	InTx(context.Context, func(context.Context) error) error
}
