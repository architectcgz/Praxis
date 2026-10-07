package bindings

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	"praxis/internal/request"
	"praxis/internal/service"
)

// 本文件定义 binding 层向应用服务请求的窄接口集。
//
// 接口定义在消费方（此处），实现在 praxis/internal/service；只描述能力，
// 不包含传输代码、DTO 与错误词汇。

// ProjectService 暴露 Project、Workspace 列表与 Project 创建。
type ProjectService interface {
	ListProjects(context.Context, int) ([]service.ProjectSummary, error)
	CreateProject(context.Context, request.CreateProject) (service.CreateProjectResult, error)
}

// SessionService 暴露 Session 列表、创建与详情视图。
type SessionService interface {
	ListSessionsByProject(context.Context, contracts.ProjectID, int) ([]service.SessionSummary, error)
	CreateSession(context.Context, request.CreateSession) (service.CreateSessionResult, error)
	GetSessionDetail(context.Context, contracts.SessionID, int) (service.SessionDetail, error)
	GetSessionUsageSummary(context.Context, contracts.SessionID) (service.SessionUsageSummary, error)
	DeleteSession(context.Context, contracts.SessionID) error
	RenameSession(context.Context, request.RenameSession) error
	PreviewFile(context.Context, request.FilePreview) (service.FilePreview, error)
}

// AgentService 暴露 Agent 详情和专属消息流。
type AgentService interface {
	GetAgentDetail(context.Context, contracts.AgentID, int) (service.AgentDetail, error)
	ListAgentMessages(context.Context, contracts.AgentID, int) ([]service.AgentMessage, error)
	ListAgentHistory(context.Context, contracts.AgentID, int) ([]service.AgentHistoryItem, error)
}

// AgentCommands 接收持久化的 Agent 执行与控制命令。
type AgentCommands interface {
	SendInput(context.Context, request.SendInput) (service.SendInputResult, error)
	PauseAgent(context.Context, request.Control) (service.ControlResult, error)
	CancelTask(context.Context, request.Control) (service.ControlResult, error)
	EnqueueTask(context.Context, request.EnqueueTask) (service.EnqueueTaskResult, error)
}

// ModelCatalog 列出已确认的模型标签与能力。
type ModelCatalog interface {
	ListModels() []service.ModelOption
}

// ModelConfigService 提供模型配置读写、凭据管理和模型发现用例。
type ModelConfigService interface {
	ModelConfig() service.ModelConfig
	SaveModelConfig(request.SaveModelConfig) error
	ReloadConfig(context.Context) error
	SetProviderKey(string, string) error
	HasProviderKey(string) bool
	DiscoverProviderModels(context.Context, string) ([]string, error)
}

// AgentEventSource 流出单次 task 的瞬时 runtime 事件。
type AgentEventSource interface {
	SubscribeAgentEvents(func(service.AgentEvent)) func()
}

// TimingService 独立查询计时事实，不读取或改写消息正文。
type TimingService interface {
	ListAgent(context.Context, string, int) ([]service.OperationTiming, error)
}

// UsageService 查询会话完整的请求用量，不受消息和计时分页影响。
type UsageService interface {
	ListSession(context.Context, string) ([]service.ModelUsageRecord, error)
}

// Services 是 binding 层所需的全部窄能力。
type Services struct {
	Projects    ProjectService
	Sessions    SessionService
	Agents      AgentService
	Commands    AgentCommands
	Models      ModelCatalog
	ModelConfig ModelConfigService
	Events      AgentEventSource
	Timings     TimingService
	Usages      UsageService
	// SessionLogPath 由持久化实现解析日志路径，前端不能指定任意本地文件。
	SessionLogPath func(context.Context, contracts.SessionID) (string, error)
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
		return errors.New("frontend model configuration service is required")
	case s.Events == nil:
		return errors.New("frontend agent event source is required")
	case s.Timings == nil:
		return errors.New("frontend timing service is required")
	case s.Usages == nil:
		return errors.New("frontend token usage service is required")
	case s.SessionLogPath == nil:
		return errors.New("frontend session log path resolver is required")
	default:
		return nil
	}
}
