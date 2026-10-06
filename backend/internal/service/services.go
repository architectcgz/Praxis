// Package service 实现桌面 binding 所依赖的前端端口集。
//
// 它是 Wails binding 与具体用例服务之间唯一的适配点：将前端端口签名转换为 service 用例参数，
// 不直接访问 repository 或 store，也不依赖 Wails 包。
package service

import (
	"praxis/internal/contracts"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	workspacemodel "praxis/internal/core/workspace"

	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"praxis/internal/agent_runtime"
	"praxis/internal/logging"
	appmodelconfig "praxis/internal/modelconfig"
	applicationagent "praxis/internal/service/agent"
	applicationproject "praxis/internal/service/project"
	applicationruntime "praxis/internal/service/runtime"
	taskqueue "praxis/internal/service/runtime/queue"
	taskstart "praxis/internal/service/runtime/task/start"
	applicationsession "praxis/internal/service/session"
)

// Config 收集实现前端端口所需的 service 与配置适配器。
type Config struct {
	Agents       *applicationagent.Service
	Projects     *applicationproject.Service
	Sessions     *applicationsession.Service
	Runtime      *applicationruntime.Service
	Models       appmodelconfig.Editor
	AgentConfig  appmodelconfig.AgentDefinitions
	ReloadConfig func(context.Context) error
	Events       *EventPublisher
	Logger       *logging.Logger
}

// Services 是前端端口的实现，由组合根装配后交给 wails 层。
type Services struct {
	agents       *applicationagent.Service
	projects     *applicationproject.Service
	sessions     *applicationsession.Service
	runtime      *applicationruntime.Service
	models       appmodelconfig.Editor
	agentConfig  appmodelconfig.AgentDefinitions
	reloadConfig func(context.Context) error
	configMu     sync.RWMutex
	events       *EventPublisher
	logger       *logging.Logger
}

// New 组装实现。可选依赖（AgentConfig、Events）留空时会退化为安全默认行为。
func New(config Config) *Services {
	return &Services{
		agents:       config.Agents,
		projects:     config.Projects,
		sessions:     config.Sessions,
		runtime:      config.Runtime,
		models:       config.Models,
		agentConfig:  config.AgentConfig,
		reloadConfig: config.ReloadConfig,
		events:       config.Events,
		logger:       logging.NewFactory().Ensure(config.Logger),
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

// GetSessionUsageSummary 返回会话已持久化用量的加权缓存率与同一份明细快照。
func (s *Services) GetSessionUsageSummary(ctx context.Context, sessionID contracts.SessionID) (applicationsession.SessionUsageSummary, error) {
	return s.sessions.GetSessionUsageSummary(ctx, sessionID)
}

// DeleteSession 删除指定会话和关联文档。
func (s *Services) DeleteSession(ctx context.Context, sessionID contracts.SessionID) error {
	return s.sessions.DeleteSession(ctx, sessionID)
}

// PreviewFile 返回当前会话工作区内的文本快照；越界、非文本或过大文件返回错误。
func (s *Services) PreviewFile(ctx context.Context, sessionID contracts.SessionID, path string) (applicationsession.FilePreview, error) {
	return s.sessions.PreviewFile(ctx, sessionID, path)
}

// RenameSession 更新指定会话的标题。
func (s *Services) RenameSession(ctx context.Context, sessionID contracts.SessionID, title string) error {
	return s.sessions.RenameSession(ctx, sessionID, title)
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
) ([]sessionmodel.MessageData, error) {
	return s.agents.ListAgentMessages(ctx, agentID, limit)
}

// CreateProject 承担前端端口与用例参数之间的差异：端口接收位置参数，用例接收
// Params。项目目录的规范化与创建属于适配层职责，core 只校验稳定引用和状态。
// 落库失败时只回收本次新建的空目录：用户可能直接选中已有目录，递归删除会丢数据。
func (s *Services) CreateProject(ctx context.Context, projectID contracts.ProjectID, workspaceID contracts.WorkspaceID, name, path string, requestID contracts.RequestID) (result applicationproject.CreateProjectResult, err error) {
	name = strings.TrimSpace(name)
	path = filepath.Clean(strings.TrimSpace(path))
	if name == "" || strings.ContainsAny(name, "\\/:*?\"<>|\x00\r\n") || name == "." || name == ".." || !filepath.IsAbs(path) {
		return result, contracts.New(contracts.ProjectWorkspaceInvalid, "")
	}
	// 只有目录原本不存在时才允许失败后回收；已存在（哪怕为空）视为用户既有路径。
	_, statErr := os.Stat(path)
	created := errors.Is(statErr, os.ErrNotExist)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return result, fmt.Errorf("创建项目目录 %s: %w", path, err)
	}
	result, err = s.projects.CreateProject(ctx, applicationproject.CreateProjectParams{
		RequestID: requestID, ProjectID: projectID, WorkspaceID: workspaceID, Name: name, Path: path,
	})
	if err != nil && created {
		// os.Remove 只删空目录：已被写入内容时保留现场，失败只记日志，不覆盖原始错误。
		if removeErr := os.Remove(path); removeErr != nil {
			s.logger.Warnf("回收未落库的项目目录失败 path=%s err=%v", path, removeErr)
		}
	}
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
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.sessions.CreateSessionForProject(ctx, sessionID, agentID, requestID, projectID, workspaceID, definitionID)
}

func (s *Services) SendInput(ctx context.Context, params taskstart.SendInputParams) (applicationruntime.StartResult, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	params.Content = strings.TrimSpace(params.Content)
	params.ProviderID = strings.TrimSpace(params.ProviderID)
	params.ModelID = strings.TrimSpace(params.ModelID)
	params.ReasoningLevel = strings.TrimSpace(params.ReasoningLevel)
	if params.ProviderID == "" || params.ModelID == "" {
		return applicationruntime.StartResult{}, contracts.New(contracts.ModelNotConfigured, "")
	}
	return s.runtime.SendInput(ctx, params)
}

func (s *Services) PauseAgent(
	ctx context.Context,
	params applicationagent.ControlParams,
) (applicationagent.ControlResult, error) {
	return s.agents.PauseAgent(ctx, params)
}

func (s *Services) CancelTask(
	ctx context.Context,
	params applicationagent.ControlParams,
) (applicationagent.ControlResult, error) {
	return s.agents.StopAgent(ctx, params)
}

func (s *Services) EnqueueTask(ctx context.Context, params taskqueue.EnqueueParams) (applicationruntime.EnqueueResult, error) {
	params.Prompt = strings.TrimSpace(params.Prompt)
	return s.runtime.EnqueueTask(ctx, params)
}

// ListModels 返回已确认的模型能力，并补上引用该模型的所有 Agent。
func (s *Services) ListModels() []appmodelconfig.Option {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
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
	s.configMu.Lock()
	defer s.configMu.Unlock()
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
	s.configMu.Lock()
	defer s.configMu.Unlock()
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

// SubscribeAgentEvents 注册瞬时 Agent runtime 事件的监听者。收到 task_ended 或 request_canceled 后，
// 消费方应重新查询持久化消息和执行状态。
func (s *Services) SubscribeAgentEvents(observer agentruntime.AgentEventObserver) func() {
	if s == nil || s.events == nil {
		return func() {}
	}
	return s.events.Subscribe(observer)
}

// EventPublisher 扇出瞬时 Agent runtime 事件，不拥有持久化状态。
// 调用方应在结算后重新查询消息和执行状态。
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
	if p == nil || event.AgentID == "" || event.TaskID == "" {
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

// ReloadConfig 重新读取全部配置，校验失败时保留当前运行时快照。
func (s *Services) ReloadConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.reloadConfig == nil {
		return errors.New("configuration reload is not available")
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.reloadConfig(ctx); err != nil {
		return err
	}
	s.logger.Infof("configuration reloaded")
	return nil
}
