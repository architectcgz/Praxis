// Package start 在 runtime 空闲时构建 Task，冻结 Context、模型和安全快照。
package start

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	"praxis/internal/core/model"
	securitymodel "praxis/internal/core/security"
	"praxis/internal/repository"
	"praxis/internal/system"
	"praxis/internal/utils/pathutil"
)

// ContextProvider 根据任务开始时的一致性快照构建初始上下文。
type ContextProvider interface {
	BuildContext(context.Context, agentmodel.Agent, string, string, string) (contextmodel.BuildResult, error)
}

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
	Transactions        repository.TxRunner
	Workspaces          repository.WorkspaceRepository
	Sessions            repository.SessionRepository
	Policies            repository.AgentSecurityPolicyRepository
	Agents              repository.SessionAgentRepository
	Tasks               repository.TaskRepository
	Executions          *agentmodel.Executions
	SessionMessages     repository.SessionMessageRepository
	AgentMessages       repository.AgentMessageRepository
	PrimaryAgent        PrimaryAgentProvider
	ToolPermissions     securitymodel.ToolPermissionPolicy
	RegisteredTools     []contracts.ToolName
	Definitions         AgentDefinitionProvider
	AgentDefinitionsDir string
	Models              ModelSnapshotFactory
	ContextProvider     ContextProvider
	Clock               system.Clock
	IDs                 system.IDGenerator
}

// Service 为直接输入创建 Task，并在 runtime 空闲时准备预约 Task 的执行快照。
type Service struct {
	tx                  repository.TxRunner
	workspaces          repository.WorkspaceRepository
	sessions            repository.SessionRepository
	policies            repository.AgentSecurityPolicyRepository
	agents              repository.SessionAgentRepository
	tasks               repository.TaskRepository
	executions          *agentmodel.Executions
	sessionMessages     repository.SessionMessageRepository
	agentMessages       repository.AgentMessageRepository
	primary             PrimaryAgentProvider
	security            SecurityResolver
	securityMu          sync.RWMutex
	models              ModelSnapshotFactory
	definitions         AgentDefinitionProvider
	agentDefinitionsDir string
	contextProvider     ContextProvider
	clock               system.Clock
	ids                 system.IDGenerator
}

// NewService 校验 Task 构建依赖；配置目录必须已经是规范化绝对路径。
func NewService(config Config) (*Service, error) {
	if config.Executions == nil {
		return nil, errors.New("task builder executions are required")
	}
	if !pathutil.IsAbsoluteNormalized(config.AgentDefinitionsDir) {
		return nil, errors.New("task agent definitions directory must be an absolute normalized path")
	}
	for name, value := range map[string]any{
		"transactions":     config.Transactions,
		"workspaces":       config.Workspaces,
		"sessions":         config.Sessions,
		"policies":         config.Policies,
		"agents":           config.Agents,
		"tasks":            config.Tasks,
		"session messages": config.SessionMessages,
		"agent messages":   config.AgentMessages,
		"primary agent":    config.PrimaryAgent,
		"definitions":      config.Definitions,
		"models":           config.Models,
		"context":          config.ContextProvider,
	} {
		if value == nil {
			return nil, fmt.Errorf("task builder %s is required", name)
		}
	}
	baseline, err := systemSecurityBaseline(config.RegisteredTools)
	if err != nil {
		return nil, err
	}
	security, err := NewSecurityResolver(baseline, config.ToolPermissions)
	if err != nil {
		return nil, err
	}
	return &Service{
		tx:                  config.Transactions,
		workspaces:          config.Workspaces,
		sessions:            config.Sessions,
		policies:            config.Policies,
		agents:              config.Agents,
		tasks:               config.Tasks,
		executions:          config.Executions,
		sessionMessages:     config.SessionMessages,
		agentMessages:       config.AgentMessages,
		primary:             config.PrimaryAgent,
		security:            security,
		models:              config.Models,
		definitions:         config.Definitions,
		agentDefinitionsDir: config.AgentDefinitionsDir,
		contextProvider:     config.ContextProvider,
		clock:               system.ClockOrDefault(config.Clock),
		ids:                 system.IDsOrDefault(config.IDs),
	}, nil
}

// UpdateToolPermissions 仅影响后续构建的 Task，不改变正在执行的安全快照。
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

// PrimaryAgentProvider 解析直接输入所属会话的 main agent。
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, contracts.SessionID, contracts.RequestID) (agentmodel.Agent, error)
}
