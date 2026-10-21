package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	"praxis/internal/core/message"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
)

type Dependencies struct {
	Rules          ApprovalRules
	Review         func(context.Context, agentmodel.Execution, message.ToolCall, json.RawMessage, string, security.ApprovalDecision, executionmodel.RiskReview) (security.RiskAssessment, error)
	Prepare        func(tool.Tool, json.RawMessage, string) (json.RawMessage, error)
	Bind           func(tool.Tool, string) tool.Tool
	HookDispatcher *hook.Dispatcher
	Transactions   repository.Transaction
	Tasks          repository.AgentExecution
	Events         repository.ToolExecution
	Messages       repository.Message
	Calls          repository.ToolCallQueries
	Sessions       repository.Session
	Agents         repository.Agent
	Catalog        tool.ToolCatalog
	Clock          clock.Clock
}

// Service 拥有授权与消息提交；活动实例表只用于取消，不领取工具运行权。
type Service struct {
	deps      Dependencies
	clock     clock.Clock
	controlMu sync.Mutex
	active    map[executionmodel.Key]*executionmodel.Execution
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Rules.DefaultMode == nil || deps.Rules.Catalog == nil || deps.Review == nil || deps.Prepare == nil || deps.Bind == nil || deps.Transactions == nil || deps.Tasks == nil || deps.Events == nil || deps.Messages == nil || deps.Calls == nil || deps.Sessions == nil || deps.Agents == nil || deps.Catalog == nil {
		return nil, errors.New("tool service dependencies are required")
	}
	return &Service{deps: deps, clock: clock.ClockOrDefault(deps.Clock), active: make(map[executionmodel.Key]*executionmodel.Execution)}, nil
}

// Scope 绑定项目环境和本次执行的审批等待能力，Loop 只发起完整工具调用。
type Scope struct {
	service       *Service
	workspacePath string
	awaitApproval func(context.Context, message.ToolCall) error
}

func (s *Service) ForWorkspace(path string, awaitApproval func(context.Context, message.ToolCall) error) *Scope {
	return &Scope{service: s, workspacePath: path, awaitApproval: awaitApproval}
}

func (s *Service) call(ctx context.Context, executionID identity.AgentExecutionID, callID string) (message.ToolCall, error) {
	return s.deps.Calls.GetCall(ctx, executionmodel.Key{ExecutionID: executionID, CallID: callID})
}
