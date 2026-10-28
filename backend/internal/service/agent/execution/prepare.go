package execution

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
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
)

// prepareMainTask 将内存 Task 准入为 Execution，开始时才向 Session 追加用户消息。
// 调用方已保留执行槽位；返回值的 bool 表示重复请求，失败后的队列与忙闲状态由调度器处理。
func (s *Service) prepareMainTask(ctx context.Context, input taskmodel.Task, session taskmodel.Session, modelConfig modelapi.Config) (result agentexecution.ExecutionRecord, existing bool, err error) {
	if modelConfig.ProviderID == "" || modelConfig.ModelID == "" {
		return result, false, modelconfig.ErrNotConfigured
	}
	// 请求身份只在调度准入时建立；由 Task ID 派生以保证准备重试使用同一身份。
	requestID := identity.RequestID(input.ID)
	inputEntryID := "input:" + requestID.String()
	previous, err := s.records.FindByRequest(ctx, session.MainAgentID(), requestID)
	if err == nil {
		stored, err := s.history.GetEntry(ctx, session.ID, previous.InputEntryID)
		if err != nil {
			return result, false, err
		}
		if previous.SourceTaskID != input.ID || stored.Role != role.User || stored.TextContent() != input.Prompt {
			return result, false, errorcode.ErrRequestConflict
		}
		if previous.Active() {
			if err := s.history.AdmitInput(ctx, session.ID, previous.InputEntryID); err != nil {
				return result, false, err
			}
		}
		return previous, true, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return result, false, err
	}
	definition, err := s.definitions.Definition(agentmodel.DefinitionPrimary)
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
	if previous, err := s.history.GetEntry(ctx, session.ID, inputEntryID); err == nil {
		if previous.Role != role.User || previous.RequestID != requestID.String() || previous.SourceTaskID != input.ID.String() || previous.ExecutionID == "" || previous.TextContent() != input.Prompt {
			return result, false, errorcode.ErrRequestConflict
		}
		executionID = identity.AgentExecutionID(previous.ExecutionID)
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return result, false, err
	}
	stored, _, err := s.history.AppendEntry(ctx, session.ID, sessionentry.ContextEntry{
		ID:           inputEntryID,
		RequestID:    requestID.String(),
		SourceTaskID: input.ID.String(),
		ExecutionID:  executionID.String(),
		Role:         role.User,
		AuthorKind:   sessionentry.AuthorUser,
		Content:      []sessionentry.Block{{Kind: sessionentry.BlockText, Text: input.Prompt}},
		CreatedAt:    at,
	})
	if err != nil {
		return result, false, err
	}
	if stored.ExecutionID != executionID.String() || stored.RequestID != requestID.String() || stored.SourceTaskID != input.ID.String() || stored.Role != role.User {
		return result, false, errorcode.ErrRequestConflict
	}
	result = agentexecution.NewExecutionRecord(executionID, session.ID, session.MainAgentID(), requestID, inputEntryID, config, at)
	result.SourceTaskID = input.ID
	if err = s.records.Save(ctx, result); err != nil {
		return agentexecution.ExecutionRecord{}, false, err
	}
	if err := s.history.AdmitInput(ctx, session.ID, stored.ID); err != nil {
		return result, false, err
	}
	// Execution 保存成功后发布输入；通知独立于提交后 Hook，也不依赖 Loop 启动。
	if s.emitter != nil {
		s.emitter.Emit(coreevent.Event{
			Scope: coreevent.Scope{
				SessionID:   result.SessionID,
				AgentID:     result.AgentID,
				ExecutionID: result.ID,
			},
			Payload: coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{stored}},
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
