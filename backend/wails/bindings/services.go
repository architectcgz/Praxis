package bindings

import (
	"context"
	"errors"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	modelconfig "praxis/internal/core/model/config"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	execution "praxis/internal/service/agent/coordinator"
	approvalservice "praxis/internal/service/approval"
	modelservice "praxis/internal/service/model"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	toolservice "praxis/internal/service/toolexecution"
	workspaceservice "praxis/internal/service/workspace"
)

// 接口只声明 Wails 实际消费的业务能力，协议投影留在 binding。
type ProjectService interface {
	ListProjects(context.Context, int) ([]projectmodel.Project, error)
	CreateProject(context.Context, projectservice.CreateInput) (projectservice.CreateProjectResult, error)
}

type ApprovalService interface {
	GetApprovalMode(context.Context, identity.ProjectID) (approvalservice.ApprovalSettings, error)
	SetApprovalMode(context.Context, identity.ProjectID, *security.ApprovalMode) (approvalservice.ApprovalSettings, error)
}

type SessionService interface {
	ListSessionsByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.Session, error)
	CreateSession(context.Context, sessionservice.CreateInput) (sessionservice.CreateResult, error)
	GetSessionView(context.Context, identity.SessionID, int) (sessionservice.SessionView, error)
	DeleteSession(context.Context, identity.SessionID) error
	RenameSession(context.Context, identity.SessionID, string) error
	SelectedModel(context.Context, identity.SessionID) (sessionmodel.ModelChoice, error)
}

type WorkspaceService interface {
	PreviewFile(context.Context, identity.SessionID, string) (workspaceservice.FilePreview, error)
}

type AgentService interface {
	GetAgentView(context.Context, identity.AgentID, int) (agentservice.AgentView, error)
	ListAgentEntries(context.Context, identity.AgentID) ([]sessionentry.ContextEntry, error)
	PauseAgent(context.Context, agentservice.ControlInput) (agentservice.ControlResult, error)
	StopAgent(context.Context, agentservice.ControlInput) (agentservice.ControlResult, error)
}

type AgentCommands interface {
	ListToolApprovals(context.Context, identity.AgentID) ([]toolservice.Approval, error)
	StopToolCall(context.Context, toolservice.ControlRequest) error
	ApproveToolCall(context.Context, execution.ToolDecision) error
	DenyToolCall(context.Context, execution.ToolDecision) error
	SubmitTask(context.Context, sessionservice.TaskInput) (execution.SubmitResult, error)
	CancelTask(context.Context, identity.SessionID, identity.TaskID) error
	SelectModel(context.Context, identity.SessionID, sessionmodel.ModelChoice) error
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

type TimingService interface {
	ListAgentTimings(context.Context, string, int) ([]timing.View, error)
}

type UsageService interface {
	ListSessionUsage(context.Context, identity.SessionID) ([]providerapi.UsageRecord, error)
	GetSessionUsageSummary(context.Context, identity.SessionID) (sessionservice.SessionUsageSummary, error)
}

// Services 由 Bootstrap 注入业务对象和只读日志定位能力，不创建中间应用层。
type Services struct {
	Projects       ProjectService
	Approvals      ApprovalService
	Sessions       SessionService
	Workspaces     WorkspaceService
	Agents         AgentService
	Commands       AgentCommands
	Models         ModelCatalog
	ModelConfig    ModelConfigService
	Emitter        *coreevent.Emitter
	Timings        TimingService
	Usages         UsageService
	SessionLogPath func(context.Context, identity.SessionID) (string, error)
}

// Validate 在宿主接入边界检查完整装配，失败时不开放 binding。
func (s Services) Validate() error {
	for name, missing := range map[string]bool{
		"projects":            s.Projects == nil,
		"approvals":           s.Approvals == nil,
		"sessions":            s.Sessions == nil,
		"workspaces":          s.Workspaces == nil,
		"agents":              s.Agents == nil,
		"commands":            s.Commands == nil,
		"models":              s.Models == nil,
		"model configuration": s.ModelConfig == nil,
		"event emitter":       s.Emitter == nil,
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
