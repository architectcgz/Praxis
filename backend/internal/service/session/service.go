// Package session 负责会话加载、配置管理、恢复流程和已加载实例定位。
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
	Log            LogStore
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 在选择入口加载并登记会话；页面的分支选择与输入队列由 Coordinator 持有。
type Service struct {
	loadConfig     func(context.Context) (*Configuration, error)
	hookDispatcher *hook.Dispatcher
	projects       repository.Project
	sessions       repository.Session
	log            sessionmodel.EventLog
	clock          clock.Clock
	ids            identity.IDGenerator

	// openedMu 保护实例注册表与创建登记，不覆盖 Entry 操作。
	openedMu        sync.Mutex
	opened          map[identity.SessionID]*openedSession
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
		opened:          make(map[identity.SessionID]*openedSession),
		hookDispatcher:  deps.HookDispatcher,
		projects:        deps.Projects,
		sessions:        deps.Sessions,
		log:             eventLog{store: deps.Log},
		clock:           deps.Clock,
		ids:             deps.IDs,
		createdSessions: make(map[identity.SessionID]struct{}),
	}
}

// OpenedSession 只定位已经加载的核心会话，不触发日志读取或恢复。
func (s *Service) OpenedSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.session, nil
}

func (s *Service) openedSession(ctx context.Context, id identity.SessionID) (*openedSession, error) {
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
func (s *Service) LoadSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	s.openedMu.Lock()
	defer s.openedMu.Unlock()
	if owner := s.opened[id]; owner != nil {
		return owner.session, nil
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
	data := sessionmodel.Open(info.ID)
	replay := logReplay{session: data, callIndexes: make(map[sessionentry.Key]int)}
	if err := s.log.Read(ctx, id, data.MainAgentID(), replay.apply); err != nil {
		return nil, err
	}
	agents, err := s.log.Subagents(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, agentID := range agents {
		data.SubagentEntries[agentID] = nil
		if err := s.log.Read(ctx, id, agentID, replay.apply); err != nil {
			return nil, err
		}
	}
	if err := data.ValidateExecutions(); err != nil {
		return nil, err
	}
	owner := sessionmodel.New(data, s.log, s.clock, s.ids)
	if len(data.Branches) == 0 {
		if _, err := owner.CreateBranch(ctx, "", sessionmodel.BranchInput{RequestID: "initial", Name: "main"}); err != nil {
			return nil, err
		}
	}
	if err := replay.repairToolResults(ctx, owner, info.State); err != nil {
		return nil, err
	}
	for _, record := range slices.Clone(data.Executions) {
		if !record.Active() {
			continue
		}
		at := s.clock.Now()
		if record.Status == agentexecution.ExecutionStarting {
			record.Start(at)
		}
		record.End(agentexecution.ExecutionInterrupted, "", at)
		if err := owner.SaveExecution(ctx, record); err != nil {
			return nil, err
		}
	}
	s.opened[id] = &openedSession{
		session: owner,
		config:  config,
	}
	return owner, nil
}

// ReleaseSession 由 Coordinator 确认页面和执行均已退出后调用，不删除持久化记录。
func (s *Service) ReleaseSession(ctx context.Context, id identity.SessionID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.openedMu.Lock()
	defer s.openedMu.Unlock()
	owner := s.opened[id]
	if owner == nil {
		return nil
	}
	// 非空事实不能依赖实例常驻，否则退出清理会把已释放的新会话当成空会话。
	if !owner.session.Empty() {
		delete(s.createdSessions, id)
	}
	delete(s.opened, id)
	return nil
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
	clear(s.opened)
	s.openedMu.Unlock()
	return cleanupErr
}

func (s *Service) deleteSession(ctx context.Context, sessionID identity.SessionID, onlyEmpty bool) error {
	s.openedMu.Lock()
	defer s.openedMu.Unlock()
	owner := s.opened[sessionID]
	// 本进程新建但尚未加载的会话只有空日志，可以直接清理。
	if onlyEmpty && owner != nil && !owner.session.Empty() {
		return nil
	}
	if err := s.sessions.Delete(ctx, sessionID); err != nil {
		return err
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
