package bindings

import (
	"context"
	"errors"

	agentcontracts "praxis/internal/app/agent/contracts"
	modelcontracts "praxis/internal/app/model/contracts"
	projectcontracts "praxis/internal/app/project/contracts"
	runtimecontracts "praxis/internal/app/runtime/contracts"
	sessioncontracts "praxis/internal/app/session/contracts"
	timingcontracts "praxis/internal/app/timing/contracts"
	usagecontracts "praxis/internal/app/usage/contracts"
	"praxis/internal/request"
)

// 本文件定义 binding 消费的窄应用接口，实现在各业务 app。
// 接口不暴露业务实现、内核类型、传输 DTO 或存储格式。

// ProjectService 提供项目目录、创建和授权设置的应用入口。
type ProjectService interface {
	ListProjects(context.Context, int) ([]projectcontracts.ProjectSummary, error)
	CreateProject(context.Context, request.CreateProject) (projectcontracts.CreateProjectResult, error)
	GetProjectApprovalMode(context.Context, string) (projectcontracts.ProjectApprovalMode, error)
	SetProjectApprovalMode(context.Context, request.SetProjectApprovalMode) (projectcontracts.ProjectApprovalMode, error)
}

// SessionService 提供会话目录、详情、管理及受限文件访问的应用入口。
type SessionService interface {
	ListSessionsByProject(context.Context, string, int) ([]sessioncontracts.SessionSummary, error)
	CreateSession(context.Context, request.CreateSession) (sessioncontracts.CreateSessionResult, error)
	GetSessionDetail(context.Context, string, int) (sessioncontracts.SessionDetail, error)
	DeleteSession(context.Context, string) error
	RenameSession(context.Context, request.RenameSession) error
	PreviewFile(context.Context, request.FilePreview) (sessioncontracts.FilePreview, error)
	SessionLogPath(context.Context, string) (string, error)
}

// AgentService 提供 Agent 查询及固定目标的执行控制，不暴露进程内执行绑定。
type AgentService interface {
	GetAgentDetail(context.Context, string, int) (agentcontracts.AgentDetail, error)
	ListAgentMessages(context.Context, string, int) ([]agentcontracts.AgentMessage, error)
	ListAgentHistory(context.Context, string, string, int) (agentcontracts.AgentHistoryPage, error)
	PauseAgent(context.Context, request.Control) (agentcontracts.ControlResult, error)
	CancelTask(context.Context, request.Control) (agentcontracts.ControlResult, error)
}

// AgentCommands 接收任务提交和工具审批，不直接执行工具。
type AgentCommands interface {
	ListToolApprovals(context.Context, string) ([]runtimecontracts.ToolApproval, error)
	ApproveToolCall(context.Context, request.ToolDecision) error
	DenyToolCall(context.Context, request.ToolDecision) error
	SendInput(context.Context, request.SendInput) (runtimecontracts.SendInputResult, error)
	EnqueueTask(context.Context, request.EnqueueTask) (runtimecontracts.EnqueueTaskResult, error)
}

// ModelCatalog 列出已确认的模型标签与能力。
type ModelCatalog interface {
	ListModels() []modelcontracts.ModelOption
}

// ModelConfigService 提供模型配置、凭据和目录发现的应用入口。
type ModelConfigService interface {
	ModelConfig() modelcontracts.ModelConfig
	SaveModelConfig(request.SaveModelConfig) error
	ReloadConfig(context.Context) error
	SetProviderKey(request.SetProviderKey) error
	DiscoverProviderModels(context.Context, string) ([]string, error)
}

// AgentEventSource 流出瞬时通知，持久化事实仍通过查询获得。
type AgentEventSource interface {
	SubscribeAgentEvents(func(agentcontracts.AgentEvent)) func()
}

// TimingService 独立查询计时事实，不读取或改写消息正文。
type TimingService interface {
	ListAgent(context.Context, string, int) ([]timingcontracts.OperationTiming, error)
}

// UsageService 查询完整请求用量及同一业务快照的会话统计。
type UsageService interface {
	ListSession(context.Context, string) ([]usagecontracts.ModelUsageRecord, error)
	GetSessionUsageSummary(context.Context, string) (usagecontracts.SessionUsageSummary, error)
}

// Services 聚合 binding 实际消费的应用能力，不实现业务转发方法。
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
}

// Validate 在宿主接入边界检查全部应用能力已注入。
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
	default:
		return nil
	}
}
