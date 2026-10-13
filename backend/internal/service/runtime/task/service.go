// Package task 负责 Task 创建、准备、恢复绑定、进度持久化和终态结算，不负责执行调度。
package task

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	securitymodel "praxis/internal/core/security"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
	"praxis/internal/logging"
	"praxis/internal/repository"
	"praxis/internal/system"
	"praxis/internal/utils/pathutil"
)

// AgentDefinitionProvider 提供已校验的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// ModelSnapshotFactory 在任务开始时冻结选定模型或 Agent 默认模型。
type ModelSnapshotFactory interface {
	FreezeTaskModel(string, string, string) (model.ModelSnapshot, error)
	FreezeDefaultTaskModel(contracts.AgentDefinitionID) (model.ModelSnapshot, error)
}

type Config struct {
	Controls            repository.AgentControlCommandRepository
	Transactions        repository.TxRunner
	HookDispatcher      *hooks.Dispatcher
	Logger              *logging.Logger
	EventObserver       func(TerminalEvent)
	Workspaces          repository.WorkspaceRepository
	Sessions            repository.SessionRepository
	Policies            repository.AgentSecurityPolicyRepository
	Agents              repository.SessionAgentRepository
	Tasks               repository.TaskRepository
	Executions          *agentmodel.Executions
	Messages            repository.MessageStreams
	PrimaryAgent        PrimaryAgentProvider
	ToolPermissions     securitymodel.ToolPermissionPolicy
	RegisteredTools     []contracts.ToolName
	Definitions         AgentDefinitionProvider
	AgentDefinitionsDir string
	Models              ModelSnapshotFactory
	ContextBuilder      *TaskContextBuilder
	Clock               system.Clock
	IDs                 system.IDGenerator
}

// Service 统一负责 Task 准入、冻结执行快照、恢复绑定、进度持久化及原子结算。
type Service struct {
	controls            repository.AgentControlCommandRepository
	tx                  repository.TxRunner
	hookDispatcher      *hooks.Dispatcher
	logger              *logging.Logger
	eventObserver       func(TerminalEvent)
	workspaces          repository.WorkspaceRepository
	sessions            repository.SessionRepository
	policies            repository.AgentSecurityPolicyRepository
	agents              repository.SessionAgentRepository
	tasks               repository.TaskRepository
	executions          *agentmodel.Executions
	messages            repository.MessageStreams
	primary             PrimaryAgentProvider
	security            securitymodel.SecurityResolver
	securityMu          sync.RWMutex
	models              ModelSnapshotFactory
	definitions         AgentDefinitionProvider
	agentDefinitionsDir string
	contextBuilder      *TaskContextBuilder
	clock               system.Clock
	ids                 system.IDGenerator
}

// NewService 创建统一 Task 服务；缺少依赖或配置目录不是规范化绝对路径时返回错误。
func NewService(config Config) (*Service, error) {
	if config.Executions == nil {
		return nil, errors.New("task service executions are required")
	}
	if config.ContextBuilder == nil {
		return nil, errors.New("task service context builder is required")
	}
	if !pathutil.IsAbsoluteNormalized(config.AgentDefinitionsDir) {
		return nil, errors.New("task agent definitions directory must be an absolute normalized path")
	}
	for name, value := range map[string]any{
		"transactions":  config.Transactions,
		"controls":      config.Controls,
		"workspaces":    config.Workspaces,
		"sessions":      config.Sessions,
		"policies":      config.Policies,
		"agents":        config.Agents,
		"tasks":         config.Tasks,
		"messages":      config.Messages,
		"primary agent": config.PrimaryAgent,
		"definitions":   config.Definitions,
		"models":        config.Models,
	} {
		if value == nil {
			return nil, fmt.Errorf("task service %s is required", name)
		}
	}
	baseline, err := systemSecurityBaseline(config.RegisteredTools)
	if err != nil {
		return nil, err
	}
	security, err := securitymodel.NewSecurityResolver(baseline, config.ToolPermissions)
	if err != nil {
		return nil, err
	}
	return &Service{
		controls:            config.Controls,
		tx:                  config.Transactions,
		hookDispatcher:      config.HookDispatcher,
		logger:              logging.NewFactory().Ensure(config.Logger),
		eventObserver:       config.EventObserver,
		workspaces:          config.Workspaces,
		sessions:            config.Sessions,
		policies:            config.Policies,
		agents:              config.Agents,
		tasks:               config.Tasks,
		executions:          config.Executions,
		messages:            config.Messages,
		primary:             config.PrimaryAgent,
		security:            security,
		models:              config.Models,
		definitions:         config.Definitions,
		agentDefinitionsDir: config.AgentDefinitionsDir,
		contextBuilder:      config.ContextBuilder,
		clock:               system.ClockOrDefault(config.Clock),
		ids:                 system.IDsOrDefault(config.IDs),
	}, nil
}

// UpdateToolPermissions 更新当前工具策略；冻结快照不变，后续工具准入仍受当前限制约束。
func (s *Service) UpdateToolPermissions(policy securitymodel.ToolPermissionPolicy) {
	s.securityMu.Lock()
	defer s.securityMu.Unlock()
	s.security.ToolPermissions = policy
}

// ReleaseExecution 释放准入时建立的进程内执行绑定；只清理匹配的 Task，
// 不会取消或删除同一 Agent 后续建立的绑定。
func (s *Service) ReleaseExecution(agentID contracts.AgentID, taskID contracts.TaskID) {
	s.executions.End(agentID, taskID)
}

// Abort 结算未被 runtime 接管的 Task 并释放匹配的执行绑定；结算失败时保留绑定。
func (s *Service) Abort(ctx context.Context, taskID contracts.TaskID) error {
	if ctx == nil {
		return errors.New("task abort context is required")
	}
	ended, err := s.Settle(ctx, taskID, taskmodel.TaskInterrupted, contracts.TaskFailureInterrupted, "")
	if err != nil {
		return err
	}
	s.ReleaseExecution(ended.AgentID, taskID)
	return nil
}

// PrimaryAgentProvider 解析直接输入所属会话的 main agent。
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, contracts.SessionID, contracts.RequestID) (agentmodel.Agent, error)
}
