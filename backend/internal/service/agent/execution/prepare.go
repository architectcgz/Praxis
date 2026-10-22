package execution

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	taskmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

// PrepareTask 将内存 Task 准入为 Execution，开始时才保存用户消息并冻结上下文。
// 返回值的 bool 表示重复请求；失败释放本次预留，队列是否重入由上层决定。
func (s *Service) PrepareTask(ctx context.Context, input taskmodel.Task, agentID identity.AgentID, modelConfig modelapi.Config) (result agentmodel.Execution, existing bool, err error) {
	if ctx == nil {
		return result, false, errors.New("task preparation context is required")
	}
	if modelConfig.ProviderID == "" || modelConfig.ModelID == "" {
		return result, false, fault.New(modelconfig.ModelNotConfigured, "")
	}
	// 请求身份只在调度准入时建立；由 Task ID 派生以保证准备重试使用同一身份。
	requestID := identity.RequestID(input.ID)
	inputEntryID := "input:" + requestID.String()
	owner, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return result, false, err
	}
	if owner.SessionID != input.SessionID {
		return result, false, fault.ErrRequestConflict
	}
	previous, err := s.records.FindByRequest(ctx, owner.ID, requestID)
	if err == nil {
		stored, err := s.entries.Get(ctx, agentservice.EntryStream(owner), previous.InputEntryID)
		if err != nil {
			return result, false, err
		}
		if previous.SourceTaskID != input.ID || stored.Role != role.User || stored.TextContent() != input.Prompt {
			return result, false, fault.ErrRequestConflict
		}
		return previous, true, nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return result, false, err
	}
	if err := sessionservice.RequireWritable(ctx, s.sessions, owner.SessionID); err != nil {
		return result, false, err
	}
	if !owner.State.Startable() {
		return result, false, agentmodel.ErrAgentExecuting
	}
	count, err := s.records.CountActiveBySession(ctx, owner.SessionID)
	if err != nil {
		return result, false, err
	}
	if count != 0 {
		return result, false, agentmodel.ErrAgentExecuting
	}
	definition, err := s.definitions.Definition(owner.DefinitionID)
	if err != nil {
		return result, false, err
	}
	if definition.Profile != owner.Profile {
		return result, false, fault.ErrRevisionConflict
	}
	config := agentmodel.ExecutionConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             modelConfig,
	}
	at := s.clock.Now()
	executionID := identity.AgentExecutionID(s.ids.New("execution"))
	if previous, err := s.entries.Get(ctx, agentservice.EntryStream(owner), inputEntryID); err == nil {
		if previous.Role != role.User || previous.RequestID != requestID.String() || previous.SourceTaskID != input.ID.String() || previous.ExecutionID == "" || previous.TextContent() != input.Prompt {
			return result, false, fault.ErrRequestConflict
		}
		executionID = identity.AgentExecutionID(previous.ExecutionID)
	} else if !errors.Is(err, fault.ErrNotFound) {
		return result, false, err
	}
	_, admitted, err := s.executions.Reserve(ctx, owner.ID, executionID)
	if err != nil {
		return result, false, err
	}
	if !admitted {
		return result, false, fault.ErrRequestConflict
	}
	defer func() {
		if err != nil {
			s.executions.Release(owner.ID, executionID)
		}
	}()
	stored, _, err := s.entries.Append(ctx, sessionentry.Record{
		Stream:  agentservice.EntryStream(owner),
		AgentID: owner.ID,
		Data: sessionentry.ContextEntry{
			ID:           inputEntryID,
			RequestID:    requestID.String(),
			SourceTaskID: input.ID.String(),
			ExecutionID:  executionID.String(),
			Role:         role.User,
			AuthorKind:   sessionentry.AuthorUser,
			Content:      []sessionentry.Block{{Kind: sessionentry.BlockText, Text: input.Prompt}},
			CreatedAt:    at,
		},
	})
	if err != nil {
		return result, false, err
	}
	result = agentmodel.NewExecution(executionID, owner.SessionID, owner.ID, requestID, inputEntryID, config, at)
	result.SourceTaskID = input.ID
	if err = s.records.Save(ctx, result); err != nil {
		return agentmodel.Execution{}, false, err
	}
	if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hook.CommittedEvent{
		Identity: hook.Identity{
			SessionID:    result.SessionID,
			AgentID:      result.AgentID,
			SourceTaskID: result.SourceTaskID,
			ExecutionID:  result.ID,
		},
		Entry: &stored,
	}); hookErr != nil {
		s.logger.Errorf("user message committed hook failed: %v", hookErr)
	}
	return result, false, nil
}
