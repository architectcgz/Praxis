package bindings

import (
	"context"
	"errors"
	coreevent "praxis/internal/core/event"
	modelconfig "praxis/internal/core/model/config"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	approvalservice "praxis/internal/service/approval"
	execution "praxis/internal/service/coordinator"
	modelservice "praxis/internal/service/model"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	toolservice "praxis/internal/service/toolexecution"
)

// 接口只声明 Wails 实际消费的业务能力，协议投影留在 binding。
type ProjectService interface {
	ListProjects(context.Context, int) ([]projectmodel.Project, error)
	CreateProject(context.Context, projectservice.CreateInput) (projectmodel.Project, error)
	PreviewFile(context.Context, identity.SessionID, string) (projectservice.FilePreview, error)
}

type ApprovalService interface {
	GetApprovalMode(context.Context, identity.SessionID) (approvalservice.ApprovalSettings, error)
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode) (approvalservice.ApprovalSettings, error)
}

type SessionService interface {
	ListSessionsByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.SessionInfo, error)
	GetSessionInfo(context.Context, identity.SessionID) (sessionmodel.SessionInfo, error)
	RenameSession(context.Context, identity.SessionID, string) error
	SelectedModel(context.Context, identity.SessionID) (sessionmodel.ModelChoice, error)
	ModelOptions(context.Context, identity.SessionID) ([]modelconfig.Option, error)
}

type AgentService interface {
	ListSessionAgents(context.Context, identity.SessionID, identity.BranchID, int) ([]agentservice.AgentView, error)
	GetAgentView(context.Context, identity.SessionID, identity.AgentID, identity.BranchID) (agentservice.AgentView, error)
}

type AgentCommands interface {
	CreateSession(context.Context, sessionservice.CreateInput) (sessionservice.CreateResult, error)
	SelectSession(context.Context, identity.SessionID, identity.BranchID) (execution.BindingView, error)
	CloseSession(context.Context, identity.SessionID) error
	CreateBranch(context.Context, identity.SessionID, identity.BranchID, string, sessionmodel.BranchInput) (execution.BindingView, error)
	SwitchBranch(context.Context, identity.SessionID, identity.BranchID, string, string) (execution.BindingView, error)
	ListBranches(context.Context, identity.SessionID) ([]execution.BranchView, error)
	Tree(context.Context, identity.SessionID) ([]sessionmodel.TreeNode, error)
	EntriesAt(context.Context, identity.SessionID, string) ([]sessionentry.ContextEntry, error)
	DeleteSession(context.Context, identity.SessionID) error
	ListAgentEntries(context.Context, identity.SessionID, identity.AgentID, identity.BranchID) ([]sessionentry.ContextEntry, error)
	StopAgent(context.Context, identity.AgentID) error
	ListToolApprovals(context.Context, identity.AgentID, identity.BranchID) ([]toolservice.Approval, error)
	StopToolCall(context.Context, execution.ToolControl) error
	ApproveToolCall(context.Context, execution.ToolDecision) error
	DenyToolCall(context.Context, execution.ToolDecision) error
	SubmitTask(context.Context, sessionservice.TaskInput) (execution.SubmitResult, error)
	CancelTask(context.Context, identity.SessionID, string, identity.TaskID) error
	SelectModel(context.Context, identity.SessionID, sessionmodel.ModelChoice) error
	ReloadSessionConfig(context.Context, identity.SessionID) error
}

type ModelConfigService interface {
	Configuration() modelservice.ConfigSnapshot
	SaveModelConfig(modelconfig.Config) error
	SetProviderKey(string, string) error
	DiscoverProviderModels(context.Context, string) ([]string, error)
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
	Agents         AgentService
	Commands       AgentCommands
	ModelConfig    ModelConfigService
	Emitter        *coreevent.Emitter
	Usages         UsageService
	SessionLogPath func(context.Context, identity.SessionID) (string, error)
}

// Validate 在宿主接入边界检查完整装配，失败时不开放 binding。
func (s Services) Validate() error {
	for name, missing := range map[string]bool{
		"projects":            s.Projects == nil,
		"approvals":           s.Approvals == nil,
		"sessions":            s.Sessions == nil,
		"agents":              s.Agents == nil,
		"commands":            s.Commands == nil,
		"model configuration": s.ModelConfig == nil,
		"event emitter":       s.Emitter == nil,
		"usages":              s.Usages == nil,
		"session log path":    s.SessionLogPath == nil,
	} {
		if missing {
			return errors.New("frontend " + name + " is required")
		}
	}
	return nil
}
