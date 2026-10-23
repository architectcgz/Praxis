package toolexecution

import (
	"context"
	"errors"
	"praxis/internal/clock"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

type Dependencies struct {
	Rules    ApprovalRules
	Tasks    repository.AgentExecution
	Events   repository.ToolExecution
	Entries  repository.Entries
	Calls    repository.ToolCallQueries
	Sessions repository.Session
	Agents   repository.Agent
	Clock    clock.Clock
}

// Service 读取工具归属、准备审核输入并可靠保存审批事实和回执。
type Service struct {
	repository.ToolExecution
	deps  Dependencies
	clock clock.Clock
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Rules.DefaultMode == nil || deps.Rules.Catalog == nil || deps.Tasks == nil || deps.Events == nil || deps.Entries == nil || deps.Calls == nil || deps.Sessions == nil || deps.Agents == nil {
		return nil, errors.New("tool service dependencies are required")
	}
	return &Service{
		ToolExecution: deps.Events,
		deps:          deps,
		clock:         clock.ClockOrDefault(deps.Clock),
	}, nil
}

func (s *Service) call(ctx context.Context, executionID identity.AgentExecutionID, callID string) (sessionentry.ToolCall, error) {
	return s.deps.Calls.GetCall(ctx, executionmodel.Key{ExecutionID: executionID, CallID: callID})
}

// ResolveCall 对照已提交消息和 Execution 校验控制请求归属。
func (s *Service) ResolveCall(ctx context.Context, request executionmodel.ControlRequest) (sessionentry.ToolCall, agentexecution.Execution, error) {
	call, err := s.call(ctx, request.ExecutionID, request.CallID)
	if err != nil {
		return call, agentexecution.Execution{}, err
	}
	if request.SessionID != "" && request.SessionID != call.SessionID || request.AgentID != "" && request.AgentID != call.AgentID {
		return call, agentexecution.Execution{}, errorcode.ErrRequestConflict
	}
	execution, err := s.deps.Tasks.Get(ctx, call.ExecutionID)
	if err == nil && (execution.AgentID != call.AgentID || execution.SessionID != call.SessionID || execution.RequestID.String() != call.RequestID) {
		err = errorcode.ErrRequestConflict
	}
	return call, execution, err
}
