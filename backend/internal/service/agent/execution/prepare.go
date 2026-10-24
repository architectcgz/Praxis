package execution

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	taskmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

// PrepareTask 将内存 Task 准入为 Execution，开始时才保存用户消息并冻结上下文。
// 调用方已标记 Agent 忙碌；返回值的 bool 表示重复请求，失败后的队列与忙闲状态由调度器处理。
func (s *Service) PrepareTask(ctx context.Context, input taskmodel.Task, agentID identity.AgentID, modelConfig modelapi.Config) (result agentexecution.Execution, existing bool, err error) {
	if ctx == nil {
		return result, false, errors.New("task preparation context is required")
	}
	if modelConfig.ProviderID == "" || modelConfig.ModelID == "" {
		return result, false, modelconfig.ErrNotConfigured
	}
	// 请求身份只在调度准入时建立；由 Task ID 派生以保证准备重试使用同一身份。
	requestID := identity.RequestID(input.ID)
	inputEntryID := "input:" + requestID.String()
	owner, err := s.agent(agentID)
	if err != nil {
		return result, false, err
	}
	if owner.SessionID != input.SessionID {
		return result, false, errorcode.ErrRequestConflict
	}
	previous, err := s.records.FindByRequest(ctx, owner.ID, requestID)
	if err == nil {
		stored, err := s.entries.Get(ctx, owner.ID, previous.InputEntryID)
		if err != nil {
			return result, false, err
		}
		if previous.SourceTaskID != input.ID || stored.Role != role.User || stored.TextContent() != input.Prompt {
			return result, false, errorcode.ErrRequestConflict
		}
		if previous.Active() {
			if err := owner.BeginExecution(previous); err != nil {
				return result, false, err
			}
		}
		return previous, true, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return result, false, err
	}
	if err := sessionservice.RequireWritable(ctx, s.sessions, owner.SessionID); err != nil {
		return result, false, err
	}
	definition, err := s.definitions.Definition(owner.DefinitionID)
	if err != nil {
		return result, false, err
	}
	config := agentexecution.ExecutionConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             modelConfig,
	}
	at := s.clock.Now()
	executionID := identity.AgentExecutionID(s.ids.New("execution"))
	if previous, err := s.entries.Get(ctx, owner.ID, inputEntryID); err == nil {
		if previous.Role != role.User || previous.RequestID != requestID.String() || previous.SourceTaskID != input.ID.String() || previous.ExecutionID == "" || previous.TextContent() != input.Prompt {
			return result, false, errorcode.ErrRequestConflict
		}
		executionID = identity.AgentExecutionID(previous.ExecutionID)
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return result, false, err
	}
	stored, _, err := s.entries.Append(ctx, sessionentry.Record{
		SessionID: owner.SessionID,
		AgentID:   owner.ID,
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
	result = agentexecution.NewExecution(executionID, owner.SessionID, owner.ID, requestID, inputEntryID, config, at)
	result.SourceTaskID = input.ID
	if err = s.records.Save(ctx, result); err != nil {
		return agentexecution.Execution{}, false, err
	}
	if err := owner.BeginExecution(result); err != nil {
		return result, false, err
	}
	// Execution 保存成功后发布输入；通知独立于提交后 Hook，也不依赖 Loop 启动。
	if s.emitter != nil {
		s.emitter.Emit(coreevent.Event{
			Kind:        coreevent.EntriesAppended,
			SessionID:   result.SessionID,
			AgentID:     result.AgentID,
			ExecutionID: result.ID,
			Entries:     []sessionentry.ContextEntry{stored},
		})
	}
	if hookErr := s.hookDispatcher.UserInputCommitted(context.WithoutCancel(ctx), hook.EntryCommitted{
		Identity: hook.Identity{
			SessionID:    result.SessionID,
			AgentID:      result.AgentID,
			SourceTaskID: result.SourceTaskID,
			ExecutionID:  result.ID,
		},
		Entry: stored,
	}); hookErr != nil {
		s.logger.Errorf("user message committed hook failed: %v", hookErr)
	}
	return result, false, nil
}
