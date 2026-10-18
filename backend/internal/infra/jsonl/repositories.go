package jsonl

import (
	agentmodel "praxis/internal/service/agent"
	projectmodel "praxis/internal/service/project"
	sessionmodel "praxis/internal/service/session"

	"praxis/internal/core/toolcall"
	txrunner "praxis/internal/transaction"
	workspacemodel "praxis/internal/workspace"
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
	Messages       MessageStreams
}

// Repositories 返回共享提交边界与位置索引的仓储集合。
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
		Messages:       MessageStreams{s},
	}
}

var (
	_ txrunner.Runner           = (*Store)(nil)
	_ projectmodel.Store        = ProjectRepository{}
	_ workspacemodel.Store      = WorkspaceRepository{}
	_ sessionmodel.Store        = SessionRepository{}
	_ agentmodel.Store          = AgentRepository{}
	_ agentmodel.ExecutionStore = AgentExecutionRepository{}
	_ toolcall.Store            = ToolExecutionRepository{}
	_ sessionmodel.ContextStore = ContextRepository{}
	_ agentmodel.ControlStore   = ControlRepository{}
)
