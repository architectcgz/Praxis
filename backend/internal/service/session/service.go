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
	ConfigMu       *sync.RWMutex
	ModelConfigs   ModelConfigResolver
	HookDispatcher *hook.Dispatcher
	Projects       repository.Project
	Sessions       repository.Session
	HistoryLog     repository.SessionLog
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 持有主会话历史、Context、模型选择和输入队列，不持有 Agent 或 Execution 运行实例。
type Service struct {
	configMu       *sync.RWMutex
	modelConfigs   ModelConfigResolver
	hookDispatcher *hook.Dispatcher
	projects       repository.Project
	sessions       repository.Session
	historyLog     repository.SessionLog
	clock          clock.Clock
	ids            identity.IDGenerator

	selectionMu     sync.RWMutex
	modelSelections map[identity.SessionID]sessionmodel.ModelSelection
	taskQueues      map[identity.SessionID]*sessionmodel.TaskQueue

	// 注册表锁只负责定位会话，加载和读写由各会话自己的锁协调。
	historiesMu sync.Mutex
	histories   map[identity.SessionID]*sessionHistory

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
	Session sessionmodel.Session
}

func NewService(deps Dependencies) *Service {
	return &Service{
		configMu:        deps.ConfigMu,
		modelConfigs:    deps.ModelConfigs,
		modelSelections: make(map[identity.SessionID]sessionmodel.ModelSelection),
		taskQueues:      make(map[identity.SessionID]*sessionmodel.TaskQueue),
		histories:       make(map[identity.SessionID]*sessionHistory),
		hookDispatcher:  deps.HookDispatcher,
		projects:        deps.Projects,
		sessions:        deps.Sessions,
		historyLog:      deps.HistoryLog,
		clock:           deps.Clock,
		ids:             deps.IDs,
		createdSessions: make(map[identity.SessionID]struct{}),
	}
}

// SessionView 不包含运行实例；Agent 展示由执行查询服务投影。
type SessionView struct {
	Session sessionmodel.Session
	Tasks   []sessionmodel.Task
}

func (s *Service) ListSessionsByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return s.sessions.ListByProject(ctx, projectID, limit)
}

func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return s.sessions.List(ctx, limit)
}

// GetSessionView 打开会话并恢复 Context，不恢复旧 Execution 或创建 Agent。
func (s *Service) GetSessionView(ctx context.Context, sessionID identity.SessionID, limit int) (SessionView, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	if _, err := s.ContextEntries(ctx, sessionID); err != nil {
		return SessionView{}, err
	}
	return SessionView{
		Session: session,
		Tasks:   s.ListTasks(sessionID, limit),
	}, nil
}

// CreateSession 保存元数据并建立空历史；重复请求复用会话，不创建运行实例。
// OperationHook 在创建锁之外观察提交结果。
func (s *Service) CreateSession(ctx context.Context, params CreateInput) (result CreateResult, err error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
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
	session := sessionmodel.NewSession(params.SessionID, project.ID, s.clock.Now())
	if err := s.sessions.Create(ctx, session); err != nil {
		return CreateResult{}, err
	}
	s.createdSessions[params.SessionID] = struct{}{}
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
	s.selectionMu.Lock()
	for _, queue := range s.taskQueues {
		queue.Clear()
	}
	clear(s.taskQueues)
	s.selectionMu.Unlock()
	s.historiesMu.Lock()
	clear(s.histories)
	s.historiesMu.Unlock()
	return cleanupErr
}

func (s *Service) deleteSession(ctx context.Context, sessionID identity.SessionID, onlyEmpty bool) error {
	s.selectionMu.Lock()
	defer s.selectionMu.Unlock()
	h := s.historyFor(sessionID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if onlyEmpty {
		if h.state == nil {
			if err := s.loadHistory(ctx, sessionID, h); err != nil {
				if errors.Is(err, errorcode.ErrNotFound) {
					return nil
				}
				return err
			}
		}
		if len(h.state.entries) != 0 {
			return nil
		}
	}
	if err := s.sessions.Delete(ctx, sessionID); err != nil {
		return err
	}
	h.state, h.context = nil, nil
	s.historiesMu.Lock()
	delete(s.histories, sessionID)
	s.historiesMu.Unlock()
	delete(s.modelSelections, sessionID)
	if queue := s.taskQueues[sessionID]; queue != nil {
		queue.Clear()
		delete(s.taskQueues, sessionID)
	}
	return nil
}

func (s *Service) RenameSession(ctx context.Context, sessionID identity.SessionID, title string) error {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.sessions.UpdateTitle(ctx, sessionID, session.Title, title, s.clock.Now())
}
