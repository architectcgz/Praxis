package bindings

import (
	"context"
	"errors"
	"praxis/internal/engine/session"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/project"
	"praxis/internal/security"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	"praxis/internal/task"
)

// 接口只声明 Wails 实际消费的业务能力，协议投影留在 binding。
type ProjectService interface {
	ListProjects(context.Context, int) ([]project.Project, error)
	CreateProject(context.Context, projectservice.CreateInput) (project.Project, error)
}

type SessionService interface {
	CreateSession(context.Context, sessionservice.CreateInput) (sessionservice.CreateResult, error)
	DeleteSession(context.Context, identity.SessionID) error
	ListSessionsByProject(context.Context, identity.ProjectID, int) ([]session.SessionInfo, error)
	RenameSession(context.Context, identity.SessionID, string) error
	SessionLogPath(context.Context, identity.SessionID) (string, error)
	SelectSession(context.Context, identity.SessionID) (sessionservice.SessionView, error)
	CloseSession(context.Context, identity.SessionID) error
	AgentHistory(context.Context, identity.SessionID, identity.AgentID, *int64, int) (session.HistoryPage, error)
	SelectModel(context.Context, identity.SessionID, model.Choice) error
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode) (sessionservice.ApprovalSettings, error)
	ReloadSessionConfig(context.Context, identity.SessionID) error
	StopRun(context.Context, identity.SessionID, identity.RunID) error
	StopToolCall(context.Context, sessionservice.ToolControl) error
	ApproveToolCall(context.Context, sessionservice.ToolDecision) error
	DenyToolCall(context.Context, sessionservice.ToolDecision) error
	SubmitTask(context.Context, task.Input) (sessionservice.SubmitResult, error)
	CancelTask(context.Context, identity.SessionID, identity.TaskID) error
	PauseQueue(context.Context, identity.SessionID) error
	ResumeQueue(context.Context, identity.SessionID) error
}

// Services 是桌面 binding 调用的业务服务集。
type Services struct {
	Projects ProjectService
	Sessions SessionService
}

// Validate 在宿主接入边界检查完整装配，失败时不开放 binding。
func (s Services) Validate() error {
	for name, missing := range map[string]bool{
		"projects": s.Projects == nil,
		"sessions": s.Sessions == nil,
	} {
		if missing {
			return errors.New("frontend " + name + " is required")
		}
	}
	return nil
}
