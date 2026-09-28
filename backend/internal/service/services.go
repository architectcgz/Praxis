// Package service 实现桌面 binding 所依赖的前端端口集。
//
// 它是 Wails binding 与 core 之间唯一的适配点：将前端端口签名转换为 service 用例参数，
// 不直接访问 repository 或 store，也不依赖 Wails 包。
package service

import (
	"praxis/internal/contracts"
	projectmodel "praxis/internal/project"
	sessionmodel "praxis/internal/session"
	workspacemodel "praxis/internal/workspace"

	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"praxis/internal/logging"
	appmodelconfig "praxis/internal/modelconfig"
	runtimecontract "praxis/internal/runtime"
	agentruntime "praxis/internal/runtime/agent"
	applicationagent "praxis/internal/service/agent"
	executioncontrol "praxis/internal/service/execution/control"
	executionqueue "praxis/internal/service/execution/queue"
	executionsettlement "praxis/internal/service/execution/settlement"
	executionstart "praxis/internal/service/execution/start"
	applicationproject "praxis/internal/service/project"
	applicationsession "praxis/internal/service/session"
)

// Config 收集实现前端端口所需的 service 与配置适配器。
type Config struct {
	Agents      *applicationagent.Service
	Projects    *applicationproject.Service
	Sessions    *applicationsession.Service
	Controls    *executioncontrol.Service
	Queues      *executionqueue.Service
	Settlements *executionsettlement.Service
	Starts      *executionstart.Service
	Models      appmodelconfig.Editor
	AgentConfig appmodelconfig.AgentDefinitions
	Events      *EventPublisher
	Logger      *logging.Logger
}

// Services 是前端端口的实现，由组合根装配后交给 wails 层。
type Services struct {
	agents      *applicationagent.Service
	projects    *applicationproject.Service
	sessions    *applicationsession.Service
	controls    *executioncontrol.Service
	queues      *executionqueue.Service
	settlements *executionsettlement.Service
	starts      *executionstart.Service
	models      appmodelconfig.Editor
	agentConfig appmodelconfig.AgentDefinitions
	events      *EventPublisher
	logger      *logging.Logger
}

// New 组装实现。可选依赖（AgentConfig、Events）留空时会退化为安全默认行为。
func New(config Config) *Services {
	return &Services{
		agents:      config.Agents,
		projects:    config.Projects,
		sessions:    config.Sessions,
		controls:    config.Controls,
		queues:      config.Queues,
		settlements: config.Settlements,
		starts:      config.Starts,
		models:      config.Models,
		agentConfig: config.AgentConfig,
		events:      config.Events,
		logger:      logging.NewFactory().Ensure(config.Logger),
	}
}

func (s *Services) ListProjects(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	return s.projects.ListProjects(ctx, limit)
}

func (s *Services) ListWorkspaces(ctx context.Context, projectID contracts.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	return s.projects.ListWorkspaces(ctx, projectID, limit)
}

func (s *Services) ListSessionsByProject(
	ctx context.Context,
	projectID contracts.ProjectID,
	limit int,
) ([]sessionmodel.Session, error) {
	return s.sessions.ListSessionsByProject(ctx, projectID, limit)
}

func (s *Services) ListSessions(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return s.sessions.ListSessions(ctx, limit)
}

func (s *Services) GetSessionView(
	ctx context.Context,
	sessionID contracts.SessionID,
	limit int,
) (applicationsession.SessionView, error) {
	return s.sessions.GetSessionView(ctx, sessionID, limit)
}

func (s *Services) GetAgentView(
	ctx context.Context,
	agentID contracts.AgentID,
	limit int,
) (applicationagent.AgentView, error) {
	return s.agents.GetAgentView(ctx, agentID, limit)
}

func (s *Services) ListAgentMessages(
	ctx context.Context,
	agentID contracts.AgentID,
	limit int,
) ([]runtimecontract.AgentSessionMessage, error) {
	return s.agents.ListAgentMessages(ctx, agentID, limit)
}

// CreateProject 承担前端端口与用例参数之间的差异：端口接收位置参数，用例接收
// Params。项目目录的规范化与创建属于适配层职责，core 只校验稳定引用和状态。
func (s *Services) CreateProject(ctx context.Context, projectID contracts.ProjectID, workspaceID contracts.WorkspaceID, name, path string, requestID contracts.RequestID) (result applicationproject.CreateProjectResult, err error) {
	name = strings.TrimSpace(name)
	path = filepath.Clean(strings.TrimSpace(path))
	if name == "" || strings.ContainsAny(name, "\\/:*?\"<>|\x00\r\n") || name == "." || name == ".." || !filepath.IsAbs(path) {
		return result, contracts.New(contracts.ProjectWorkspaceInvalid, "")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return result, err
	}
	result, err = s.projects.CreateProject(ctx, applicationproject.CreateProjectParams{
		RequestID: requestID, ProjectID: projectID, WorkspaceID: workspaceID, Name: name, Path: path,
	})
	return result, err
}

func (s *Services) CreateSessionForProject(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	requestID contracts.RequestID,
	projectID contracts.ProjectID,
	workspaceID contracts.WorkspaceID,
	definitionID contracts.AgentDefinitionID,
) (applicationsession.CreateResult, error) {
	return s.sessions.CreateSessionForProject(ctx, sessionID, agentID, requestID, projectID, workspaceID, definitionID)
}

func (s *Services) SendInput(ctx context.Context, params executionstart.SendInputParams) (executionstart.Result, error) {
	agent, built, err := s.sessions.BuildExecutionContext(
		ctx, params.SessionID, params.AgentID, params.RequestID, params.Content,
	)
	if err != nil {
		return executionstart.Result{}, err
	}
	params.AgentID = agent.ID
	params.Context = built
	return s.starts.SendInput(ctx, params)
}

func (s *Services) Resume(ctx context.Context, params executionstart.ResumeParams) (executionstart.Result, error) {
	agent, built, err := s.sessions.BuildExecutionContext(
		ctx, "", params.AgentID, params.RequestID, params.Content,
	)
	if err != nil {
		return executionstart.Result{}, err
	}
	params.AgentID = agent.ID
	params.Context = built
	return s.starts.Resume(ctx, params)
}

func (s *Services) PauseAgent(
	ctx context.Context,
	params executioncontrol.Params,
) (executioncontrol.Result, error) {
	return s.controls.PauseAgent(ctx, params)
}

func (s *Services) CloseAgent(
	ctx context.Context,
	params executioncontrol.Params,
) (executioncontrol.Result, error) {
	return s.controls.CloseAgent(ctx, params)
}

func (s *Services) EnqueueWork(ctx context.Context, params executionqueue.EnqueueParams) (executionqueue.EnqueueResult, error) {
	return s.queues.EnqueueWork(ctx, params)
}

// ListModels 返回已确认的模型能力，并补上引用该模型的所有 Agent。
func (s *Services) ListModels() []appmodelconfig.Option {
	if s.models == nil {
		return []appmodelconfig.Option{}
	}
	models := s.models.ListModels()
	if s.agentConfig == nil {
		return models
	}
	for index := range models {
		models[index].AssignedAgentDefinitions = s.agentConfig.DefinitionsForModel(models[index].ProviderID, models[index].ModelID)
	}
	return models
}

// ModelConfig 返回完整模型配置的可编辑副本，供设置界面渲染。
func (s *Services) ModelConfig() appmodelconfig.Config {
	if s.models == nil {
		return appmodelconfig.Config{}
	}
	return s.models.Config()
}

// SaveModelConfig 校验并持久化整份模型配置。保存后立即对运行中的编排层生效。
func (s *Services) SaveModelConfig(config appmodelconfig.Config) error {
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	if s.agentConfig != nil {
		if err := s.agentConfig.ValidateModelConfiguration(config); err != nil {
			return err
		}
	}
	if err := s.models.Save(config); err != nil {
		return err
	}
	s.logger.Infof("model config saved providers=%d groups=%d", len(config.Providers), len(config.Groups))
	return nil
}

// SetProviderKey 写入或清除一个已配置 Provider 的 API key。
func (s *Services) SetProviderKey(providerID, value string) error {
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	if err := s.models.SetProviderKey(providerID, value); err != nil {
		return err
	}
	s.logger.Infof("provider API key updated provider=%s cleared=%t", providerID, value == "")
	return nil
}

// HasProviderKey 只报告密钥是否存在，不暴露密钥内容。
func (s *Services) HasProviderKey(providerID string) bool {
	if s.models == nil {
		return false
	}
	return s.models.HasProviderKey(providerID)
}

// DiscoverProviderModels 从已配置的 Provider 端点拉取模型 ID，解析后的 API key
// 始终留在 registry 内部。
func (s *Services) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if s.models == nil {
		return nil, errors.New("model registry is not available")
	}
	return s.models.DiscoverProviderModels(ctx, providerID)
}

// SubscribeAgentEvents 注册瞬时 Agent runtime 事件的监听者。收到 settled 通知后，
// 消费方必须改以 durable transcript 为准。
func (s *Services) SubscribeAgentEvents(observer agentruntime.AgentEventObserver) func() {
	if s == nil || s.events == nil {
		return func() {}
	}
	return s.events.Subscribe(observer)
}

// EventPublisher 扇出瞬时 Agent runtime 事件，不给它 durable ownership。
// 调用方必须在结算后改查 transcript。
type EventPublisher struct {
	mu        sync.RWMutex
	nextID    uint64
	observers map[uint64]agentruntime.AgentEventObserver
}

// NewEventPublisher 创建空的 Agent 事件扇出器。
func NewEventPublisher() *EventPublisher {
	return &EventPublisher{observers: make(map[uint64]agentruntime.AgentEventObserver)}
}

// Subscribe 注册监听者并返回注销函数。
func (p *EventPublisher) Subscribe(observer agentruntime.AgentEventObserver) func() {
	if p == nil || observer == nil {
		return func() {}
	}
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	p.observers[id] = observer
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.observers, id)
		p.mu.Unlock()
	}
}

// Publish 把一条输出事件交给当前所有监听者。
func (p *EventPublisher) Publish(event agentruntime.AgentEvent) {
	if p == nil || event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	p.mu.RLock()
	observers := make([]agentruntime.AgentEventObserver, 0, len(p.observers))
	for _, observer := range p.observers {
		observers = append(observers, observer)
	}
	p.mu.RUnlock()
	for _, observer := range observers {
		observer(event)
	}
}
