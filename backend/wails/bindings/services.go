package bindings

import (
	"context"
	"errors"
	modelservice "praxis/internal/service/model"

	"praxis/internal/core/message"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/core/toolcall"
	"praxis/internal/identity"
	"praxis/internal/security"
	"praxis/internal/service/agent"
	execution "praxis/internal/service/agent/coordinator"
	inputservice "praxis/internal/service/agent/input"
	"praxis/internal/service/project"
	"praxis/internal/service/session"
	queueservice "praxis/internal/service/task"
	"praxis/internal/timing"
)

// 接口只声明 Wails 实际消费的业务能力，协议投影留在 binding。
type ProjectService interface {
	ListProjects(context.Context, int) ([]project.Project, error)
	CreateProject(context.Context, project.CreateInput) (project.CreateProjectResult, error)
	GetApprovalMode(context.Context, identity.ProjectID) (project.ApprovalSettings, error)
	SetApprovalMode(context.Context, identity.ProjectID, *security.ApprovalMode) (project.ApprovalSettings, error)
}

type SessionService interface {
	ListSessionsByProject(context.Context, identity.ProjectID, int) ([]session.Session, error)
	CreateSession(context.Context, session.CreateInput) (session.CreateResult, error)
	GetSessionView(context.Context, identity.SessionID, int) (session.SessionView, error)
	DeleteSession(context.Context, identity.SessionID) error
	RenameSession(context.Context, identity.SessionID, string) error
	PreviewFile(context.Context, identity.SessionID, string) (session.FilePreview, error)
}

type AgentService interface {
	GetAgentView(context.Context, identity.AgentID, int) (agent.AgentView, error)
	ListAgentMessages(context.Context, identity.AgentID, int) ([]message.Message, error)
	PauseAgent(context.Context, agent.ControlInput) (agent.ControlResult, error)
	StopAgent(context.Context, agent.ControlInput) (agent.ControlResult, error)
}

type AgentCommands interface {
	ListToolApprovals(context.Context, identity.AgentID) ([]toolcall.Execution, error)
	ApproveToolCall(context.Context, execution.ToolDecision) error
	DenyToolCall(context.Context, execution.ToolDecision) error
	SendInput(context.Context, inputservice.SendInput) (execution.StartResult, error)
	EnqueueTask(context.Context, queueservice.EnqueueInput) (queueservice.EnqueueResult, error)
}

type ModelCatalog interface {
	ListModels() []modelconfig.Option
}

type ModelConfigService interface {
	Configuration() modelservice.ConfigSnapshot
	SaveModelConfig(modelconfig.Config) error
	ReloadConfig(context.Context) error
	SetProviderKey(string, string) error
	DiscoverProviderModels(context.Context, string) ([]string, error)
}

type AgentEventSource interface {
	SubscribeAgentEvents(func(execution.Event)) func()
}

type TimingService interface {
	ListAgent(context.Context, string, int) ([]timing.Record, error)
}

type UsageService interface {
	ListSessionUsage(context.Context, identity.SessionID) ([]model.ModelUsageRecord, error)
	GetSessionUsageSummary(context.Context, identity.SessionID) (session.SessionUsageSummary, error)
}

// Services 由 Bootstrap 注入业务对象和只读日志定位能力，不创建中间应用层。
type Services struct {
	Projects       ProjectService
	Sessions       SessionService
	Agents         AgentService
	Commands       AgentCommands
	Models         ModelCatalog
	ModelConfig    ModelConfigService
	Events         AgentEventSource
	Timings        TimingService
	Usages         UsageService
	SessionLogPath func(context.Context, identity.SessionID) (string, error)
}

// Validate 在宿主接入边界检查完整装配，失败时不开放 binding。
func (s Services) Validate() error {
	for name, missing := range map[string]bool{
		"projects":            s.Projects == nil,
		"sessions":            s.Sessions == nil,
		"agents":              s.Agents == nil,
		"commands":            s.Commands == nil,
		"models":              s.Models == nil,
		"model configuration": s.ModelConfig == nil,
		"events":              s.Events == nil,
		"timings":             s.Timings == nil,
		"usages":              s.Usages == nil,
		"session log path":    s.SessionLogPath == nil,
	} {
		if missing {
			return errors.New("frontend " + name + " is required")
		}
	}
	return nil
}
