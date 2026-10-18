package jsonl

import (
	agentmodel "praxis/internal/agent"
	projectmodel "praxis/internal/project"
	sessionmodel "praxis/internal/session"
	taskmodel "praxis/internal/task"
	"praxis/internal/toolcall"
	txrunner "praxis/internal/transaction"
	workspacemodel "praxis/internal/workspace"
)

// Repositories 将同一个日志事务入口提供给所有业务仓储。
type Repositories struct {
	Projects       ProjectRepository
	Workspaces     WorkspaceRepository
	Sessions       SessionRepository
	Agents         AgentRepository
	Tasks          TaskRepository
	Contexts       ContextRepository
	ToolExecutions ToolExecutionRepository
	Controls       ControlRepository
	Messages       MessageStreams
}

// Repositories 返回共享提交边界与位置索引的仓储集合。
func (s *Store) Repositories() Repositories {
	return Repositories{
		Projects:       ProjectRepository{s},
		Workspaces:     WorkspaceRepository{s},
		Sessions:       SessionRepository{s},
		Agents:         AgentRepository{s},
		Tasks:          TaskRepository{s},
		Contexts:       ContextRepository{s},
		ToolExecutions: ToolExecutionRepository{s},
		Controls:       ControlRepository{s},
		Messages:       MessageStreams{s},
	}
}

var (
	_ txrunner.Runner           = (*Store)(nil)
	_ projectmodel.Store        = ProjectRepository{}
	_ workspacemodel.Store      = WorkspaceRepository{}
	_ sessionmodel.Store        = SessionRepository{}
	_ agentmodel.Store          = AgentRepository{}
	_ taskmodel.Store           = TaskRepository{}
	_ toolcall.Store            = ToolExecutionRepository{}
	_ sessionmodel.ContextStore = ContextRepository{}
	_ agentmodel.ControlStore   = ControlRepository{}
)
