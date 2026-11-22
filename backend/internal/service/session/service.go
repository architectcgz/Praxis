package session

import (
	"context"
	"errors"
	"praxis/internal/core/approval"
	"praxis/internal/core/coordinator"
	modelapi "praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
	"time"
)

type SessionView = coordinator.SessionView
type ApprovalSettings = coordinator.ApprovalSettings
type SubmitResult = coordinator.SubmitResult
type ToolDecision = approval.DecisionRequest
type ToolControl = executionmodel.ControlRequest

type Dependencies struct {
	Core     *coordinator.Coordinator
	Projects repository.Project
	Sessions repository.Session
}

// Service 是会话与执行能力的统一应用服务入口；运行期会话句柄与并发编排由 Core 维护。
type Service struct {
	core            *coordinator.Coordinator
	projects        repository.Project
	sessions        repository.Session
	createdMu       sync.Mutex
	createdSessions map[identity.SessionID]struct{}
}

type CreateInput struct {
	SessionID identity.SessionID
	ProjectID identity.ProjectID
}

type CreateResult struct{ Session sessionmodel.SessionInfo }

func NewService(deps Dependencies) *Service {
	return &Service{
		core:            deps.Core,
		projects:        deps.Projects,
		sessions:        deps.Sessions,
		createdSessions: make(map[identity.SessionID]struct{}),
	}
}

func (s *Service) SelectSession(ctx context.Context, id identity.SessionID) (SessionView, error) {
	return s.core.SelectSession(ctx, id)
}

func (s *Service) CloseSession(ctx context.Context, id identity.SessionID) error {
	return s.core.CloseSession(ctx, id)
}

func (s *Service) ListSessionsByProject(ctx context.Context, id identity.ProjectID, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.ListByProject(ctx, id, limit)
}

func (s *Service) SessionLogPath(ctx context.Context, id identity.SessionID) (string, error) {
	return s.sessions.SessionLogPath(ctx, id)
}

func (s *Service) CreateSession(ctx context.Context, input CreateInput) (CreateResult, error) {
	if existing, err := s.sessions.Get(ctx, input.SessionID); err == nil {
		if existing.ProjectID != input.ProjectID {
			return CreateResult{}, errorcode.ErrRequestConflict
		}
		return CreateResult{Session: existing}, nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return CreateResult{}, err
	}
	project, err := s.projects.Get(ctx, input.ProjectID)
	if err != nil {
		return CreateResult{}, err
	}
	if project.State != projectmodel.ProjectActive {
		return CreateResult{}, errorcode.ErrRequestConflict
	}
	info := sessionmodel.NewSessionInfo(input.SessionID, input.ProjectID, time.Now().UTC())
	if err := s.sessions.Create(ctx, info); err != nil {
		return CreateResult{}, err
	}
	s.createdMu.Lock()
	s.createdSessions[info.ID] = struct{}{}
	s.createdMu.Unlock()
	return CreateResult{Session: info}, nil
}

func (s *Service) DeleteSession(ctx context.Context, id identity.SessionID) error {
	if err := s.core.CloseSession(ctx, id); err != nil {
		return err
	}
	if err := s.sessions.Delete(ctx, id); err != nil {
		return err
	}
	s.createdMu.Lock()
	delete(s.createdSessions, id)
	s.createdMu.Unlock()
	return nil
}

func (s *Service) RenameSession(ctx context.Context, id identity.SessionID, title string) error {
	info, err := s.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.sessions.UpdateTitle(ctx, id, info.Title, title, time.Now().UTC())
}

// CleanupEmptySessions 在所有运行期会话关闭后清理本次新建且未产生日志内容的空会话。
func (s *Service) CleanupEmptySessions(ctx context.Context) error {
	s.createdMu.Lock()
	ids := make([]identity.SessionID, 0, len(s.createdSessions))
	for id := range s.createdSessions {
		ids = append(ids, id)
	}
	s.createdMu.Unlock()
	var result error
	for _, id := range ids {
		empty, err := s.sessions.IsEmpty(ctx, id)
		if errors.Is(err, errorcode.ErrNotFound) {
			continue
		}
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if empty {
			if err := s.sessions.Delete(ctx, id); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}

func (s *Service) AgentHistory(ctx context.Context, id identity.SessionID, agentID identity.AgentID, before *int64, limit int) (sessionmodel.HistoryPage, error) {
	return s.core.AgentHistory(ctx, id, agentID, before, limit)
}

func (s *Service) SubmitTask(ctx context.Context, input taskmodel.Input) (SubmitResult, error) {
	return s.core.SubmitTask(ctx, input)
}

func (s *Service) CancelTask(ctx context.Context, sessionID identity.SessionID, taskID identity.TaskID) error {
	return s.core.CancelTask(ctx, sessionID, taskID)
}

func (s *Service) PauseQueue(ctx context.Context, id identity.SessionID) error {
	return s.core.PauseQueue(ctx, id)
}

func (s *Service) ResumeQueue(ctx context.Context, id identity.SessionID) error {
	return s.core.ResumeQueue(ctx, id)
}

func (s *Service) SelectModel(ctx context.Context, id identity.SessionID, choice modelapi.Choice) error {
	return s.core.SelectModel(ctx, id, choice)
}

func (s *Service) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	return s.core.SetApprovalMode(ctx, id, mode)
}

func (s *Service) ReloadSessionConfig(ctx context.Context, id identity.SessionID) error {
	return s.core.ReloadSessionConfig(ctx, id)
}

func (s *Service) StopRun(ctx context.Context, sessionID identity.SessionID, runID identity.RunID) error {
	return s.core.StopRun(ctx, sessionID, runID)
}

func (s *Service) ApproveToolCall(ctx context.Context, params ToolDecision) error {
	return s.core.ApproveToolCall(ctx, params)
}

func (s *Service) DenyToolCall(ctx context.Context, params ToolDecision) error {
	return s.core.DenyToolCall(ctx, params)
}

func (s *Service) StopToolCall(ctx context.Context, request ToolControl) error {
	return s.core.StopToolCall(ctx, request)
}
