package bindings

import (
	"context"
	"errors"
	execution "praxis/internal/core/coordinator"
	coreevent "praxis/internal/core/event"
	modelconfig "praxis/internal/core/model/config"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	approvalservice "praxis/internal/service/approval"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
)

// 接口只声明 Wails 实际消费的业务能力，协议投影留在 binding。
type ProjectService interface {
	ListProjects(context.Context, int) ([]projectmodel.Project, error)
	CreateProject(context.Context, projectservice.CreateInput) (projectmodel.Project, error)
}

type ApprovalService interface {
	GetApprovalMode(context.Context, identity.SessionID) (approvalservice.ApprovalSettings, error)
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode) (approvalservice.ApprovalSettings, error)
}

type SessionService interface {
	CreateSession(context.Context, sessionservice.CreateInput) (sessionservice.CreateResult, error)
	DeleteSession(context.Context, identity.SessionID) error
	ListSessionsByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.SessionInfo, error)
	GetSessionInfo(context.Context, identity.SessionID) (sessionmodel.SessionInfo, error)
	RenameSession(context.Context, identity.SessionID, string) error
	SelectedModel(context.Context, identity.SessionID) (sessionmodel.ModelChoice, error)
	ModelOptions(context.Context, identity.SessionID) ([]modelconfig.Option, error)
}

type AgentCommands interface {
	SelectSession(context.Context, identity.SessionID) (execution.SessionView, error)
	CloseSession(context.Context, identity.SessionID) error
	SessionContent(context.Context, identity.SessionID) (execution.SessionContent, error)
	AgentHistory(context.Context, identity.SessionID, identity.AgentID, *int64, int) (sessionmodel.HistoryPage, error)
	StopAgent(context.Context, execution.AgentControl) error
	ListToolApprovals(context.Context, identity.SessionID, identity.AgentID) ([]executionmodel.Approval, error)
	StopToolCall(context.Context, execution.ToolControl) error
	ApproveToolCall(context.Context, execution.ToolDecision) error
	DenyToolCall(context.Context, execution.ToolDecision) error
	SubmitTask(context.Context, taskmodel.Input) (execution.SubmitResult, error)
	CancelTask(context.Context, identity.SessionID, identity.TaskID) error
	SelectModel(context.Context, identity.SessionID, sessionmodel.ModelChoice) error
	ReloadSessionConfig(context.Context, identity.SessionID) error
	PauseQueue(context.Context, identity.SessionID) error
	ResumeQueue(context.Context, identity.SessionID) error
}

// Services 由 Bootstrap 注入业务对象和只读日志定位能力，不创建中间应用层。
type Services struct {
	Projects       ProjectService
	Approvals      ApprovalService
	Sessions       SessionService
	Commands       AgentCommands
	Emitter        *coreevent.Emitter
	SessionLogPath func(context.Context, identity.SessionID) (string, error)
}

// Validate 在宿主接入边界检查完整装配，失败时不开放 binding。
func (s Services) Validate() error {
	for name, missing := range map[string]bool{
		"projects":         s.Projects == nil,
		"approvals":        s.Approvals == nil,
		"sessions":         s.Sessions == nil,
		"commands":         s.Commands == nil,
		"event emitter":    s.Emitter == nil,
		"session log path": s.SessionLogPath == nil,
	} {
		if missing {
			return errors.New("frontend " + name + " is required")
		}
	}
	return nil
}
