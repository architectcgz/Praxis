package storage

import (
	"praxis/internal/repository"
)

// Repositories 组合共享同一内存状态与后台持久化生命周期的仓储。
type Repositories struct {
	Projects   ProjectRepository
	Workspaces WorkspaceRepository
	Sessions   SessionRepository
	Executions AgentExecutionRepository
	Entries    EntryRepository
	ToolCalls  ToolCallQueries
	ModelUsage ModelUsageRepository
}

func (s *Storage) Repositories() Repositories {
	return Repositories{
		Projects:   ProjectRepository{storage: s},
		Workspaces: WorkspaceRepository{storage: s},
		Sessions:   SessionRepository{storage: s},
		Executions: AgentExecutionRepository{storage: s},
		Entries:    EntryRepository{storage: s},
		ToolCalls:  ToolCallQueries{storage: s},
		ModelUsage: ModelUsageRepository{storage: s},
	}
}

var (
	_ repository.Project         = ProjectRepository{}
	_ repository.Workspace       = WorkspaceRepository{}
	_ repository.Session         = SessionRepository{}
	_ repository.AgentExecution  = AgentExecutionRepository{}
	_ repository.Entries         = EntryRepository{}
	_ repository.ToolCallQueries = ToolCallQueries{}
	_ repository.ModelUsage      = ModelUsageRepository{}
)
