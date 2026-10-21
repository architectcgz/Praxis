package storage

import (
	"praxis/internal/repository"
)

// Repositories 将同一个日志事务入口提供给所有业务仓储。
type Repositories struct {
	Tasks          TaskRepository
	Projects       ProjectRepository
	Workspaces     WorkspaceRepository
	Sessions       SessionRepository
	Agents         AgentRepository
	Executions     AgentExecutionRepository
	Contexts       ContextRepository
	ToolExecutions ToolExecutionRepository
	Controls       ControlRepository
	Messages       MessageRepository
	ToolCalls      ToolCallQueries
	Timings        TimingStore
	ModelUsage     ModelUsageStore
}

// Repositories 返回共享提交边界与内存 State的仓储集合。
func (s *Store) Repositories() Repositories {
	return Repositories{
		Tasks:          TaskRepository{s},
		Projects:       ProjectRepository{s},
		Workspaces:     WorkspaceRepository{s},
		Sessions:       SessionRepository{s},
		Agents:         AgentRepository{s},
		Executions:     AgentExecutionRepository{s},
		Contexts:       ContextRepository{s},
		ToolExecutions: ToolExecutionRepository{s},
		Controls:       ControlRepository{s},
		Messages:       MessageRepository{s},
		ToolCalls:      ToolCallQueries{s},
		Timings:        TimingStore{Store: s},
		ModelUsage:     ModelUsageStore{Store: s},
	}
}

var (
	_ repository.Transaction     = (*Store)(nil)
	_ repository.Project         = ProjectRepository{}
	_ repository.Workspace       = WorkspaceRepository{}
	_ repository.Session         = SessionRepository{}
	_ repository.Agent           = AgentRepository{}
	_ repository.AgentExecution  = AgentExecutionRepository{}
	_ repository.ToolExecution   = ToolExecutionRepository{}
	_ repository.SessionContext  = ContextRepository{}
	_ repository.AgentControl    = ControlRepository{}
	_ repository.Task            = TaskRepository{}
	_ repository.Message         = MessageRepository{}
	_ repository.ToolCallQueries = ToolCallQueries{}
	_ repository.Timing          = TimingStore{}
	_ repository.ModelUsage      = ModelUsageStore{}
)
