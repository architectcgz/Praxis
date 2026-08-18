package persistence

import (
	"context"

	"praxis/internal/core/domain"
)

type TaskSessionRepository interface {
	Get(ctx context.Context, id domain.TaskSessionID) (domain.TaskSession, error)
	Save(ctx context.Context, session domain.TaskSession) error
}

type TaskPacketRepository interface {
	Get(ctx context.Context, id domain.TaskPacketID) (domain.TaskPacket, error)
	Save(ctx context.Context, packet domain.TaskPacket) error
}

type ContextManifestRepository interface {
	Get(ctx context.Context, id domain.ContextManifestID) (domain.ContextManifest, error)
	Save(ctx context.Context, manifest domain.ContextManifest) error
}

type CapabilityGrantRepository interface {
	Get(ctx context.Context, id domain.CapabilityGrantID) (domain.CapabilityGrant, error)
	Save(ctx context.Context, grant domain.CapabilityGrant) error
}

type DelegationRepository interface {
	Get(ctx context.Context, id domain.DelegationRequestID) (domain.DelegationRequest, error)
	Save(ctx context.Context, request domain.DelegationRequest) error
}

type AgentThreadRepository interface {
	Get(ctx context.Context, id domain.AgentThreadID) (domain.AgentThread, error)
	Save(ctx context.Context, thread domain.AgentThread) error
}

type AgentRunRepository interface {
	Get(ctx context.Context, id domain.AgentRunID) (domain.AgentRun, error)
	Save(ctx context.Context, run domain.AgentRun) error
}

type WorkspaceLeaseRepository interface {
	GetActiveByWorkspace(ctx context.Context, workspaceKey string) (domain.WorkspaceWriteLease, error)
	Save(ctx context.Context, lease domain.WorkspaceWriteLease) error
	Release(ctx context.Context, lease domain.WorkspaceWriteLease) error
}

type AgentResultRepository interface {
	Get(ctx context.Context, id domain.AgentResultID) (domain.AgentResult, error)
	Save(ctx context.Context, result domain.AgentResult) error
}

type BriefingRepository interface {
	Get(ctx context.Context, id domain.BriefingID) (domain.Briefing, error)
	Save(ctx context.Context, briefing domain.Briefing) error
}

type DeliveryRepository interface {
	Get(ctx context.Context, id domain.DeliveryID) (domain.BriefingDelivery, error)
	GetByInjectionKey(ctx context.Context, injectionKey string) (domain.BriefingDelivery, error)
	Save(ctx context.Context, delivery domain.BriefingDelivery) error
}

type NoteRepository interface {
	Get(ctx context.Context, id domain.NoteID) (domain.Note, error)
	Save(ctx context.Context, note domain.Note) error
}

type EventRepository interface {
	Append(ctx context.Context, event domain.DomainEvent) error
}

type AgentPolicyStore interface {
	Current(ctx context.Context) (domain.AgentPolicySnapshot, error)
	Update(ctx context.Context, snapshot domain.AgentPolicySnapshot) error
}

type TxRunner interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
