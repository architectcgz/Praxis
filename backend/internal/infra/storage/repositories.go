package storage

import (
	"praxis/internal/repository"
)

// Repositories 为所有业务仓储提供同一个单事件写入入口。
type Repositories struct {
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

// Repositories 返回共享进程内互斥锁与内存 State 的仓储集合。
func (s *Store) Repositories() Repositories {
	return Repositories{
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
	_ repository.Project         = ProjectRepository{}
	_ repository.Workspace       = WorkspaceRepository{}
	_ repository.Session         = SessionRepository{}
	_ repository.Agent           = AgentRepository{}
	_ repository.AgentExecution  = AgentExecutionRepository{}
	_ repository.ToolExecution   = ToolExecutionRepository{}
	_ repository.SessionContext  = ContextRepository{}
	_ repository.AgentControl    = ControlRepository{}
	_ repository.Message         = MessageRepository{}
	_ repository.ToolCallQueries = ToolCallQueries{}
	_ repository.Timing          = TimingStore{}
	_ repository.ModelUsage      = ModelUsageStore{}
)
