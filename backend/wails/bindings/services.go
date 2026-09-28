package bindings

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	projectmodel "praxis/internal/project"
	sessionmodel "praxis/internal/session"
	workspacemodel "praxis/internal/workspace"

	appmodelconfig "praxis/internal/modelconfig"
	runtimecontract "praxis/internal/runtime"
	agentruntime "praxis/internal/runtime/agent"
	applicationagent "praxis/internal/service/agent"
	executioncontrol "praxis/internal/service/execution/control"
	executionqueue "praxis/internal/service/execution/queue"
	executionstart "praxis/internal/service/execution/start"
	applicationproject "praxis/internal/service/project"
	applicationsession "praxis/internal/service/session"
)

// 本文件定义 binding 层向 core 请求的窄接口集。
//
// 接口定义在消费方（此处），实现在 praxis/internal/service；只描述能力，
// 不包含传输代码、DTO 与错误词汇。

// ProjectService 暴露 Project、Workspace 列表与 Project 创建。
type ProjectService interface {
	ListProjects(context.Context, int) ([]projectmodel.Project, error)
	CreateProject(context.Context, contracts.ProjectID, contracts.WorkspaceID, string, string, contracts.RequestID) (applicationproject.CreateProjectResult, error)
	ListWorkspaces(context.Context, contracts.ProjectID, int) ([]workspacemodel.Workspace, error)
}

// SessionService 暴露 Session 列表、创建与详情视图。
type SessionService interface {
	ListSessions(context.Context, int) ([]sessionmodel.Session, error)
	ListSessionsByProject(context.Context, contracts.ProjectID, int) ([]sessionmodel.Session, error)
	CreateSessionForProject(context.Context, contracts.SessionID, contracts.AgentID, contracts.RequestID, contracts.ProjectID, contracts.WorkspaceID, contracts.AgentDefinitionID) (applicationsession.CreateResult, error)
	GetSessionView(context.Context, contracts.SessionID, int) (applicationsession.SessionView, error)
}

// AgentService 暴露 Agent 详情与 transcript 消息。
type AgentService interface {
	GetAgentView(context.Context, contracts.AgentID, int) (applicationagent.AgentView, error)
	ListAgentMessages(context.Context, contracts.AgentID, int) ([]runtimecontract.AgentSessionMessage, error)
}

// AgentCommands 接收持久化的 Agent 执行与控制命令。
type AgentCommands interface {
	SendInput(context.Context, executionstart.SendInputParams) (executionstart.Result, error)
	Resume(context.Context, executionstart.ResumeParams) (executionstart.Result, error)
	PauseAgent(context.Context, executioncontrol.Params) (executioncontrol.Result, error)
	CloseAgent(context.Context, executioncontrol.Params) (executioncontrol.Result, error)
	EnqueueWork(context.Context, executionqueue.EnqueueParams) (executionqueue.EnqueueResult, error)
}

// ModelCatalog 列出已确认的模型标签与能力。
type ModelCatalog interface {
	ListModels() []appmodelconfig.Option
}

// ModelConfigEditor 读写模型配置与凭据。
type ModelConfigEditor interface {
	ModelConfig() appmodelconfig.Config
	SaveModelConfig(appmodelconfig.Config) error
	SetProviderKey(string, string) error
	HasProviderKey(string) bool
	DiscoverProviderModels(context.Context, string) ([]string, error)
}

// AgentEventSource 流出单次 execution 的瞬时 runtime 事件。
type AgentEventSource interface {
	SubscribeAgentEvents(agentruntime.AgentEventObserver) func()
}

// Services 是 binding 层所需的全部窄能力。
type Services struct {
	Projects    ProjectService
	Sessions    SessionService
	Agents      AgentService
	Commands    AgentCommands
	Models      ModelCatalog
	ModelConfig ModelConfigEditor
	Events      AgentEventSource
}

// Validate 检查所有前端服务都已注入。
func (s Services) Validate() error {
	switch {
	case s.Projects == nil:
		return errors.New("frontend project service is required")
	case s.Sessions == nil:
		return errors.New("frontend session service is required")
	case s.Agents == nil:
		return errors.New("frontend agent service is required")
	case s.Commands == nil:
		return errors.New("frontend agent commands are required")
	case s.Models == nil:
		return errors.New("frontend model catalog is required")
	case s.ModelConfig == nil:
		return errors.New("frontend model configuration editor is required")
	case s.Events == nil:
		return errors.New("frontend agent event source is required")
	default:
		return nil
	}
}
