// Package session 负责 Session 初始化、会话模型选择、状态读取和上下文追加。
package session

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/hook"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	projectservice "praxis/internal/service/project"
	"slices"
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

// Service 在选择入口加载并登记会话；页面的分支选择与输入队列由 Coordinator 持有。
type Service struct {
	loadConfig     func(context.Context) (*Configuration, error)
	hookDispatcher *hook.Dispatcher
	projects       repository.Project
	sessions       repository.Session
	log            repository.SessionLog
	clock          clock.Clock
	ids            identity.IDGenerator

	// openedMu 保护实例注册表与创建登记，不覆盖 Entry 操作。
	openedMu        sync.Mutex
	opened          map[identity.SessionID]*Session
	createdSessions map[identity.SessionID]struct{}
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

// OpenedSession 只定位已经加载的实例，不触发日志读取或恢复。
func (s *Service) OpenedSession(ctx context.Context, id identity.SessionID) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.openedMu.Lock()
	owner := s.opened[id]
	s.openedMu.Unlock()
	if owner == nil {
		return nil, errors.New("session is not open")
	}
	return owner, nil
}

// LoadSession 只由选择入口调用，完成日志重放、Entry 构建和中断收尾后才发布实例。
func (s *Service) LoadSession(ctx context.Context, id identity.SessionID) (*Session, error) {
	s.openedMu.Lock()
	defer s.openedMu.Unlock()
	if owner := s.opened[id]; owner != nil {
		return owner, nil
	}
	info, err := s.sessions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	config, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	config.ApprovalMode = security.EffectiveApprovalMode(config.GlobalApprovalMode, info.ApprovalMode)
	owner := &Session{
		config: config,
		data:   sessionmodel.Open(info.ID),
		log:    s.log,
		failed: make(chan struct{}),
	}
	defer func() {
		// 不确定写入后保留故障对象，后续会话数据操作只能返回故障，直到应用重启。
		if owner.fault != nil {
			s.opened[id] = owner
		}
	}()
	replay := logReplay{session: owner.data, callIndexes: make(map[sessionentry.Key]int)}
	if err := owner.log.Read(ctx, id, replay.apply); err != nil {
		return nil, err
	}
	if len(owner.data.Branches) == 0 {
		branch := sessionmodel.Branch{
			ID:        identity.BranchID(s.ids.New("branch")),
			Name:      "main",
			RequestID: "initial",
			CreatedAt: s.clock.Now(),
			UpdatedAt: s.clock.Now(),
		}
		if err := owner.createBranch(ctx, branch); err != nil {
			return nil, err
		}
	}
	if err := replay.repairToolResults(ctx, owner, info.State); err != nil {
		return nil, err
	}
	for _, record := range slices.Clone(owner.data.Executions) {
		if !record.Active() {
			continue
		}
		at := s.clock.Now()
		if record.Status == agentexecution.ExecutionStarting {
			record.Start(at)
		}
		record.End(agentexecution.ExecutionInterrupted, "", at)
		if err := owner.saveExecution(ctx, record); err != nil {
			return nil, err
		}
	}
	s.opened[id] = owner
	return owner, nil
}

func (s *Service) ListSessionsByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.ListByProject(ctx, projectID, limit)
}

func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.SessionInfo, error) {
	return s.sessions.List(ctx, limit)
}

// GetSessionInfo 是纯元数据查询，不打开会话或加载会话数据。
func (s *Service) GetSessionInfo(ctx context.Context, sessionID identity.SessionID) (sessionmodel.SessionInfo, error) {
	return s.sessions.Get(ctx, sessionID)
}

// CreateSession 保存元数据与空日志；会话数据在选定后加载，重复请求复用原会话。
// 由 Coordinator 接纳后调用，创建和 Hook 收尾均计入关闭等待。
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
	session := sessionmodel.NewSessionInfo(params.SessionID, project.ID, s.clock.Now())
	if err := s.sessions.Create(ctx, session); err != nil {
		return CreateResult{}, err
	}
	s.openedMu.Lock()
	s.createdSessions[params.SessionID] = struct{}{}
	s.openedMu.Unlock()
	return CreateResult{Session: session}, nil
}

// DeleteSession 由 Coordinator 确认会话没有活动执行后调用。
func (s *Service) DeleteSession(ctx context.Context, sessionID identity.SessionID) error {
	return s.deleteSession(ctx, sessionID, false)
}

// CleanupEmptySessions 由 Application 在 Coordinator 关闭且所有命令退出后单线程调用。
func (s *Service) CleanupEmptySessions(ctx context.Context) error {
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
	// 本进程新建但尚未加载的会话只有空日志，可以直接清理。
	if onlyEmpty && owner != nil {
		owner.mu.RLock()
		empty := owner.checkState(ctx) == nil && len(owner.data.Entries) == 0
		owner.mu.RUnlock()
		if !empty {
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
	delete(s.createdSessions, sessionID)
	return nil
}

func (s *Service) RenameSession(ctx context.Context, sessionID identity.SessionID, title string) error {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.sessions.UpdateTitle(ctx, sessionID, session.Title, title, s.clock.Now())
}
