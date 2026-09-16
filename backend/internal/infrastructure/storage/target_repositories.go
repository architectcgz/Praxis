package storage

import (
	"context"

	domainworkflow "praxis/internal/domain/workflow"
	"praxis/internal/infrastructure/document"
	"praxis/internal/infrastructure/sqlite"
	"praxis/internal/persistence"
	sessionport "praxis/internal/session"
)

// TargetRepositories is the composition boundary between relational indexes
// and file-backed aggregate documents.
type TargetRepositories struct {
	Commands          CommandReceiptRepository
	Projects          sqlite.ProjectRepository
	Workspaces        sqlite.WorkspaceRepository
	Sessions          sqlite.SessionRepository
	Contexts          SessionContextRepository
	Policies          AgentSecurityPolicyRepository
	Agents            sqlite.AgentRepository
	Executions        AgentExecutionRepository
	SecuritySnapshots ExecutionSecuritySnapshotRepository
	ToolInvocations   ToolInvocationRepository
	QueuedWork        QueuedWorkRepository
	Waits             WaitConditionRepository
	Controls          sqlite.AgentControlCommandRepository
	Deliveries        sqlite.ContextDeliveryRepository
	Events            persistence.EventRepository
	adapter           *Store
}

// NewTargetRepositories assembles repositories without making SQLite depend on
// the document implementation.
func NewTargetRepositories(db *sqlite.Store, documents *document.Store) (TargetRepositories, error) {
	adapter, err := New(db, documents)
	if err != nil {
		return TargetRepositories{}, err
	}
	relational := db.Repositories()
	return TargetRepositories{
		Commands:          CommandReceiptRepository{adapter},
		Projects:          relational.Projects,
		Workspaces:        relational.Workspaces,
		Sessions:          relational.Sessions,
		Contexts:          SessionContextRepository{adapter},
		Policies:          AgentSecurityPolicyRepository{adapter},
		Agents:            relational.Agents,
		Executions:        AgentExecutionRepository{adapter},
		SecuritySnapshots: ExecutionSecuritySnapshotRepository{adapter},
		ToolInvocations:   ToolInvocationRepository{adapter},
		QueuedWork:        QueuedWorkRepository{adapter},
		Waits:             WaitConditionRepository{adapter},
		Controls:          relational.Controls,
		Deliveries:        relational.Deliveries,
		Events:            relational.Events,
		adapter:           adapter,
	}, nil
}

// ResolveContextArtifact resolves approved workflow content from documents.
func (r TargetRepositories) ResolveContextArtifact(ctx context.Context, delivery domainworkflow.ContextDelivery) (sessionport.ContextArtifact, error) {
	return r.adapter.ResolveContextArtifact(ctx, delivery)
}
