package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/clock"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/core/toolexecution/authorization"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
)

type Dependencies struct {
	TimingRecorder *timing.Recorder
	Rules          ApprovalRules
	Authorization  *authorization.Authorizer
	Prepare        func(tool.Tool, json.RawMessage, string) (json.RawMessage, error)
	Bind           func(tool.Tool, string) tool.Tool
	HookDispatcher *hook.Dispatcher
	Tasks          repository.AgentExecution
	Events         repository.ToolExecution
	Entries        repository.Entries
	Calls          repository.ToolCallQueries
	Sessions       repository.Session
	Agents         repository.Agent
	Catalog        tool.ToolCatalog
	Clock          clock.Clock
}

// Service 准备授权输入、保存审批事实并提交工具回执；活动实例表只用于取消。
type Service struct {
	deps      Dependencies
	clock     clock.Clock
	controlMu sync.Mutex
	active    map[executionmodel.Key]*executionmodel.Execution
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Rules.DefaultMode == nil || deps.Rules.Catalog == nil || deps.Authorization == nil || deps.Authorization.Provider == nil || deps.Authorization.Emitter == nil || deps.Prepare == nil || deps.Bind == nil || deps.Tasks == nil || deps.Events == nil || deps.Entries == nil || deps.Calls == nil || deps.Sessions == nil || deps.Agents == nil || deps.Catalog == nil {
		return nil, errors.New("tool service dependencies are required")
	}
	return &Service{deps: deps, clock: clock.ClockOrDefault(deps.Clock), active: make(map[executionmodel.Key]*executionmodel.Execution)}, nil
}

// Scope 绑定项目环境和本次执行的审批等待能力，Loop 只发起完整工具调用。
type Scope struct {
	service             *Service
	workspacePath       string
	prepareApprovalWait func(context.Context) error
	awaitApproval       func(context.Context) error
}

func (s *Service) ForWorkspace(path string, prepareApprovalWait, awaitApproval func(context.Context) error) *Scope {
	return &Scope{
		service:             s,
		workspacePath:       path,
		prepareApprovalWait: prepareApprovalWait,
		awaitApproval:       awaitApproval,
	}
}

func (s *Service) call(ctx context.Context, executionID identity.AgentExecutionID, callID string) (sessionentry.ToolCall, error) {
	return s.deps.Calls.GetCall(ctx, executionmodel.Key{ExecutionID: executionID, CallID: callID})
}
