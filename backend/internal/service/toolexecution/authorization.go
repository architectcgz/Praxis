package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/core/toolexecution/authorization"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
)

// authorize 为 Core 准备当前事实并保存其产出的事件，不执行授权决策或模型请求。
func (scope *Scope) authorize(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (bool, *tool.ToolResult, error) {
	s := scope.service
	for {
		s.controlMu.Lock()
		input, result, err := scope.prepareAuthorization(ctx, call, args)
		s.controlMu.Unlock()
		if err != nil {
			return false, nil, err
		}
		if result.ReviewRequired {
			// 模型调用期间不持有控制锁、配置锁或存储锁。
			result, err = s.deps.Authorization.Authorize(ctx, input)
			if err != nil {
				return false, nil, err
			}
		}
		if result.Event != nil {
			s.controlMu.Lock()
			stopped, err := s.saveAuthorization(ctx, *result.Event)
			s.controlMu.Unlock()
			if err != nil {
				return false, nil, err
			}
			if stopped || result.Event.Kind == executionmodel.Reviewed {
				continue
			}
		}
		return result.Pending, result.Denied, nil
	}
}

func (scope *Scope) prepareAuthorization(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (authorization.Input, authorization.Result, error) {
	s := scope.service
	input := authorization.Input{Call: call, Arguments: args}
	task, err := s.deps.Tasks.Get(ctx, call.ExecutionID)
	if err != nil {
		return input, authorization.Result{}, err
	}
	if !task.Active() || task.AgentID != call.AgentID || task.SessionID != call.SessionID {
		return input, authorization.Result{}, errorcode.ErrRequestConflict
	}
	facts, err := s.deps.Events.Read(ctx, call.Key)
	if err != nil {
		return input, authorization.Result{}, err
	}
	if !facts.StopRequestedAt.IsZero() {
		value := tool.NewToolError("tool_interrupted", "The tool call was canceled before execution.")
		return input, authorization.Result{Denied: &value}, nil
	}
	input.Facts = facts.ApprovalFacts
	input.Decision, err = s.deps.Rules.Decide(ctx, call, args)
	if err != nil {
		return input, authorization.Result{}, err
	}
	result, err := s.deps.Authorization.Authorize(ctx, input)
	if err != nil || !result.ReviewRequired {
		return input, result, err
	}
	input.Review, err = scope.prepareReview(ctx, task, call)
	return input, result, err
}

// prepareReview 在模型调用前读取输入、预留请求序号并保存用量归属。
func (scope *Scope) prepareReview(ctx context.Context, task agentexecution.Execution, call sessionentry.ToolCall) (*authorization.ReviewRequest, error) {
	s := scope.service
	owner, err := s.deps.Agents.Get(ctx, task.AgentID)
	if err != nil {
		return nil, err
	}
	if owner.SessionID != task.SessionID || call.AgentID != owner.ID {
		return nil, errorcode.ErrRequestConflict
	}
	input, err := s.deps.Entries.Get(ctx, agentservice.EntryStream(owner), task.InputEntryID)
	if err != nil {
		return nil, err
	}
	userTask := input.TextContent()
	if userTask == "" {
		return nil, errors.New("risk review cannot locate the task input")
	}
	sequence := max(uint64(1), task.Progress.NextSequence)
	task.Progress.NextSequence, task.Progress.ModelRequests = sequence+1, task.Progress.ModelRequests+1
	if err := s.deps.Tasks.Save(ctx, task); err != nil {
		return nil, err
	}
	return &authorization.ReviewRequest{
		Model:         task.Config.Model.Clone(),
		UserTask:      userTask,
		WorkspacePath: scope.workspacePath,
		Attempt: executionmodel.RiskReview{
			TurnID:     identity.NewTurnID(task.ID, sequence),
			ProviderID: task.Config.Model.ProviderID,
			ModelID:    task.Config.Model.ModelID,
			StartedAt:  s.clock.Now().UTC(),
		},
	}, nil
}

// saveAuthorization 与 Stop 共用控制锁，已停止的调用不再追加审批或审核事实。
func (s *Service) saveAuthorization(ctx context.Context, event executionmodel.Event) (bool, error) {
	facts, err := s.deps.Events.Read(ctx, event.Key)
	if err != nil {
		return false, err
	}
	if !facts.StopRequestedAt.IsZero() {
		return true, nil
	}
	return false, s.deps.Events.Append(ctx, event)
}
