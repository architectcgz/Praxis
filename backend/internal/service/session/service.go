package session

import (
	"context"
	"errors"
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
)

type SessionAccess interface {
	LoadSession(context.Context, identity.SessionID) (*sessionmodel.Session, error)
	GetSession(context.Context, identity.SessionID) (*sessionmodel.Session, error)
	CloseSession(context.Context, identity.SessionID) error
}

type Dependencies struct {
	Core           SessionAccess
	HookDispatcher *hook.Dispatcher
	Projects       repository.Project
	Sessions       repository.Session
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 组织会话应用用例；已加载对象与执行资源由 Core 管理。
type Service struct {
	core            SessionAccess
	hookDispatcher  *hook.Dispatcher
	projects        repository.Project
	sessions        repository.Session
	clock           clock.Clock
	createdMu       sync.Mutex
	createdSessions map[identity.SessionID]struct{}
}

type CreateInput struct {
	RequestID identity.RequestID
	SessionID identity.SessionID
	ProjectID identity.ProjectID
}
type CreateResult struct{ Session sessionmodel.SessionInfo }

func NewService(deps Dependencies) *Service {
	return &Service{
		core:            deps.Core,
		hookDispatcher:  deps.HookDispatcher,
		projects:        deps.Projects,
		sessions:        deps.Sessions,
		clock:           deps.Clock,
		createdSessions: make(map[identity.SessionID]struct{}),
	}
}
func (s *Service) OpenedSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	return s.core.GetSession(ctx, id)
}
func (s *Service) LoadSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	return s.core.LoadSession(ctx, id)
}
func (s *Service) ListSessionsByProject(ctx context.Context, id identity.ProjectID, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.ListByProject(ctx, id, limit)
}
func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.List(ctx, limit)
}
func (s *Service) GetSessionInfo(ctx context.Context, id identity.SessionID) (sessionmodel.SessionInfo, error) {
	return s.sessions.Get(ctx, id)
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
	info := sessionmodel.NewSessionInfo(input.SessionID, input.ProjectID, s.clock.Now())
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
	return s.sessions.UpdateTitle(ctx, id, info.Title, title, s.clock.Now())
}
func (s *Service) CleanupEmptySessions(ctx context.Context) error {
	s.createdMu.Lock()
	ids := make([]identity.SessionID, 0, len(s.createdSessions))
	for id := range s.createdSessions {
		ids = append(ids, id)
	}
	s.createdMu.Unlock()
	var result error
	for _, id := range ids {
		owner, err := s.sessions.Load(ctx, id)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if owner.Empty() {
			if err := s.sessions.Delete(ctx, id); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}
