package storage

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/repository"
)

// Repositories 组合共享同一类型化内存状态与同步日志的仓储。
type Repositories struct {
	Projects   ProjectRepository
	Workspaces WorkspaceRepository
	Sessions   SessionRepository
	Executions AgentExecutionRepository
	Entries    EntryRepository
	ToolCalls  ToolCallQueries
	ModelUsage ModelUsageRepository
}

func (s *Store) Repositories() Repositories {
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
	_ sessionentry.Store         = EntryRepository{}
	_ repository.ToolCallQueries = ToolCallQueries{}
	_ repository.ModelUsage      = ModelUsageRepository{}
)
