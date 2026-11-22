package bindings

import (
	"context"
	"errors"
	"praxis/internal/core/approval"
	coreevent "praxis/internal/core/event"
	modelapi "praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
)

// 接口只声明 Wails 实际消费的业务能力，协议投影留在 binding。
type ProjectService interface {
	ListProjects(context.Context, int) ([]projectmodel.Project, error)
	CreateProject(context.Context, projectservice.CreateInput) (projectmodel.Project, error)
}

type SessionService interface {
	CreateSession(context.Context, sessionservice.CreateInput) (sessionservice.CreateResult, error)
	DeleteSession(context.Context, identity.SessionID) error
	ListSessionsByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.SessionInfo, error)
	GetSessionInfo(context.Context, identity.SessionID) (sessionmodel.SessionInfo, error)
	RenameSession(context.Context, identity.SessionID, string) error
	SessionLogPath(context.Context, identity.SessionID) (string, error)
	SelectSession(context.Context, identity.SessionID) (sessionservice.SessionView, error)
	CloseSession(context.Context, identity.SessionID) error
	AgentHistory(context.Context, identity.SessionID, identity.AgentID, *int64, int) (sessionmodel.HistoryPage, error)
	SelectedModel(context.Context, identity.SessionID) (sessionmodel.ModelChoice, error)
	ModelOptions(context.Context, identity.SessionID) ([]modelapi.Option, error)
	SelectModel(context.Context, identity.SessionID, sessionmodel.ModelChoice) error
	GetApprovalMode(context.Context, identity.SessionID) (sessionservice.ApprovalSettings, error)
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode) (sessionservice.ApprovalSettings, error)
	ReloadSessionConfig(context.Context, identity.SessionID) error
	StopRun(context.Context, identity.SessionID, identity.RunID) error
	ListToolApprovals(context.Context, identity.SessionID, identity.AgentID) ([]approval.Approval, error)
	StopToolCall(context.Context, sessionservice.ToolControl) error
	ApproveToolCall(context.Context, sessionservice.ToolDecision) error
	DenyToolCall(context.Context, sessionservice.ToolDecision) error
	SubmitTask(context.Context, taskmodel.Input) (sessionservice.SubmitResult, error)
	CancelTask(context.Context, identity.SessionID, identity.TaskID) error
	PauseQueue(context.Context, identity.SessionID) error
	ResumeQueue(context.Context, identity.SessionID) error
}

// Services 由 Bootstrap 注入外部业务服务与事件源。
type Services struct {
	Projects ProjectService
	Sessions SessionService
	Emitter  *coreevent.Emitter
}

// Validate 在宿主接入边界检查完整装配，失败时不开放 binding。
func (s Services) Validate() error {
	for name, missing := range map[string]bool{
		"projects":      s.Projects == nil,
		"sessions":      s.Sessions == nil,
		"event emitter": s.Emitter == nil,
	} {
		if missing {
			return errors.New("frontend " + name + " is required")
		}
	}
	return nil
}
