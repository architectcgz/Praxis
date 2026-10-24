package storage

import (
	"praxis/internal/repository"
)

// Repositories 为所有业务仓储提供同一个单事件写入入口。
type Repositories struct {
	Projects       ProjectRepository
	Workspaces     WorkspaceRepository
	Sessions       SessionRepository
	Executions     AgentExecutionRepository
	ToolExecutions ToolExecutionRepository
	Entries        EntryRepository
	ToolCalls      ToolCallQueries
	ModelUsage     ModelUsageStore
}

// Repositories 返回共享进程内互斥锁与内存 State 的仓储集合。
func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:       ProjectRepository{s},
		Workspaces:     WorkspaceRepository{s},
		Sessions:       SessionRepository{s},
		Executions:     AgentExecutionRepository{s},
		ToolExecutions: ToolExecutionRepository{s},
		Entries:        EntryRepository{s},
		ToolCalls:      ToolCallQueries{s},
		ModelUsage:     ModelUsageStore{Store: s},
	}
}

var (
	_ repository.Project         = ProjectRepository{}
	_ repository.Workspace       = WorkspaceRepository{}
	_ repository.Session         = SessionRepository{}
	_ repository.AgentExecution  = AgentExecutionRepository{}
	_ repository.ToolExecution   = ToolExecutionRepository{}
	_ repository.Entries         = EntryRepository{}
	_ repository.ToolCallQueries = ToolCallQueries{}
	_ repository.ModelUsage      = ModelUsageStore{}
)
