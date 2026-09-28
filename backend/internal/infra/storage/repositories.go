package storage

import (
	"praxis/internal/infra/document"
	"praxis/internal/infra/sqlite"
)

// Repositories is the composition boundary between relational indexes
// and file-backed aggregate documents.
type Repositories struct {
	Projects          sqlite.ProjectRepository
	Workspaces        sqlite.WorkspaceRepository
	Sessions          sqlite.SessionRepository
	Contexts          SessionContextRepository
	Policies          AgentSecurityPolicyRepository
	Agents            sqlite.SessionAgentRepository
	Executions        AgentExecutionRepository
	SecuritySnapshots ExecutionSecuritySnapshotRepository
	ToolInvocations   ToolInvocationRepository
	QueuedWork        QueuedWorkRepository
	Waits             WaitConditionRepository
	Controls          sqlite.AgentControlCommandRepository
}

// NewRepositories assembles repositories without making SQLite depend on
// the document implementation.
func NewRepositories(db *sqlite.Store, documents *document.Store) (Repositories, error) {
	adapter, err := New(db, documents)
	if err != nil {
		return Repositories{}, err
	}
	relational := db.Repositories()
	return Repositories{
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
	}, nil
}
