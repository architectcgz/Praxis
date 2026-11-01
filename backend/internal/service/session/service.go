// Package session 负责 Session 初始化、会话模型选择、状态读取和上下文追加。
package session

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	projectservice "praxis/internal/service/project"
	"sync"
)

// Dependencies 包含 Session 用例、会话模型选择与上下文读写所需的依赖。
type Dependencies struct {
	LoadConfig     func(context.Context) (*Configuration, error)
	HookDispatcher *hook.Dispatcher
	Projects       repository.Project
	Sessions       repository.Session
	Log            repository.SessionLog
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 打开并登记会话；页面的分支选择与输入队列由 Coordinator 持有。
type Service struct {
	loadConfig     func(context.Context) (*Configuration, error)
	hookDispatcher *hook.Dispatcher
	projects       repository.Project
	sessions       repository.Session
	log            repository.SessionLog
	clock          clock.Clock
	ids            identity.IDGenerator

	// 注册表只保护会话的创建、查找和释放，不覆盖 Entry 操作。
	openedMu sync.Mutex
	opened   map[identity.SessionID]*Session

	createdSessionsMu sync.Mutex
	createdSessions   map[identity.SessionID]struct{}
	closing           bool
}

// CreateInput 只接收已校验的会话及归属身份，不接收 Agent 实例或角色定义。
type CreateInput struct {
	RequestID identity.RequestID
	SessionID identity.SessionID
	ProjectID identity.ProjectID
}

type CreateResult struct {
	Session sessionmodel.SessionInfo
}

func NewService(deps Dependencies) *Service {
	return &Service{
		loadConfig:      deps.LoadConfig,
		opened:          make(map[identity.SessionID]*Session),
		hookDispatcher:  deps.HookDispatcher,
		projects:        deps.Projects,
		sessions:        deps.Sessions,
		log:             deps.Log,
		clock:           deps.Clock,
		ids:             deps.IDs,
		createdSessions: make(map[identity.SessionID]struct{}),
	}
}

func (s *Service) ListSessionsByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.ListByProject(ctx, projectID, limit)
}

func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.List(ctx, limit)
}

// GetSessionInfo 是纯元数据查询，不打开页面或加载历史。
func (s *Service) GetSessionInfo(ctx context.Context, sessionID identity.SessionID) (sessionmodel.SessionInfo, error) {
	return s.sessions.Get(ctx, sessionID)
}

// CreateSession 保存元数据并建立空历史；重复请求复用会话，不创建运行实例。
// OperationHook 在创建锁之外观察提交结果。
func (s *Service) CreateSession(ctx context.Context, params CreateInput) (result CreateResult, err error) {
	ctx, scope := s.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{
			SessionID: params.SessionID,
			AgentID:   identity.AgentID(params.SessionID),
		},
		Kind: hook.OperationSessionCreate,
		Name: "create_session",
	})
	defer func() { scope.End(hook.Result{Err: err}) }()
	// 创建与退出清理串行，保证新会话不会漏过清理范围；先解锁再执行 Scope.End。
	s.createdSessionsMu.Lock()
	defer s.createdSessionsMu.Unlock()
	if s.closing {
		return CreateResult{}, errors.New("session service is closing")
	}
	existing, err := s.sessions.Get(ctx, params.SessionID)
	if err == nil {
		if existing.ProjectID != params.ProjectID {
			return CreateResult{}, errorcode.ErrRequestConflict
		}
		if _, err := s.openSession(ctx, params.SessionID); err != nil {
			return CreateResult{}, err
		}
		return CreateResult{Session: existing}, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return CreateResult{}, err
	}
	project, err := s.projects.Get(ctx, params.ProjectID)
	if err != nil {
		return CreateResult{}, err
	}
	if project.State != projectmodel.ProjectActive {
		return CreateResult{}, projectservice.ErrProjectInvalid
	}
	session := sessionmodel.NewSessionInfo(params.SessionID, project.ID, s.clock.Now())
	if err := s.sessions.Create(ctx, session); err != nil {
		return CreateResult{}, err
	}
	s.createdSessions[params.SessionID] = struct{}{}
	if _, err := s.openSession(ctx, params.SessionID); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Session: session}, nil
}

// DeleteSession 由 Coordinator 确认会话没有活动执行后调用。
func (s *Service) DeleteSession(ctx context.Context, sessionID identity.SessionID) error {
	return s.deleteSession(ctx, sessionID, false)
}

// CleanupEmptySessions 在 Coordinator 停止后清理新建的空会话，释放历史状态并拒绝后续创建。
func (s *Service) CleanupEmptySessions(ctx context.Context) error {
	s.createdSessionsMu.Lock()
	defer s.createdSessionsMu.Unlock()
	s.closing = true
	var cleanupErr error
	for sessionID := range s.createdSessions {
		if err := s.deleteSession(ctx, sessionID, true); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup session %s: %w", sessionID, err))
			continue
		}
		delete(s.createdSessions, sessionID)
	}
	s.openedMu.Lock()
	for _, owner := range s.opened {
		owner.close()
	}
	clear(s.opened)
	s.openedMu.Unlock()
	return cleanupErr
}

func (s *Service) deleteSession(ctx context.Context, sessionID identity.SessionID, onlyEmpty bool) error {
	s.openedMu.Lock()
	defer s.openedMu.Unlock()
	owner := s.opened[sessionID]
	if onlyEmpty {
		if owner == nil {
			return nil
		}
		empty := false
		if err := owner.use(ctx, func() error { empty = len(owner.data.Entries) == 0; return nil }); err != nil || !empty {
			return nil
		}
	}
	if err := s.sessions.Delete(ctx, sessionID); err != nil {
		return err
	}
	if owner != nil {
		owner.close()
	}
	delete(s.opened, sessionID)
	return nil
}

func (s *Service) RenameSession(ctx context.Context, sessionID identity.SessionID, title string) error {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.sessions.UpdateTitle(ctx, sessionID, session.Title, title, s.clock.Now())
}
