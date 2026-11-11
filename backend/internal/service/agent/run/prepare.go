package run

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
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

// prepareMainTask 将内存 Task 准入为 Run，开始时才向 Session 追加用户消息。
// 调用方已保留执行槽位；返回值的 bool 表示重复请求，失败后的队列与忙闲状态由调度器处理。
func (s *Service) prepareMainTask(ctx context.Context, input taskmodel.Task, session taskmodel.SessionInfo, owner agentrun.SessionContext, bindingID string, modelConfig modelapi.Config, definition agentmodel.AgentDefinition) (result agentrun.RunRecord, existing bool, err error) {
	if modelConfig.ProviderID == "" || modelConfig.ModelID == "" {
		return result, false, modelconfig.ErrNotConfigured
	}
	inputEntryID := "input:" + input.ID.String()
	previous, err := s.records.FindRunByTask(ctx, session.MainAgentID(), input.ID)
	if err == nil {
		if previous.SourceTaskID != input.ID || previous.BranchID != input.BranchID {
			return result, false, errorcode.ErrRequestConflict
		}
		return previous, true, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return result, false, err
	}
	config := agentrun.RunConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             modelConfig,
	}
	at := s.clock.Now()
	runID := identity.RunID(s.ids.New("execution"))
	previousInput, err := owner.Entry(ctx, inputEntryID)
	if err == nil {
		if previousInput.TextContent() != input.Prompt {
			return result, false, errorcode.ErrRequestConflict
		}
		runID = identity.RunID(previousInput.RunID)
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return result, false, err
	}
	stored, inserted, err := owner.AppendEntry(ctx, input.BranchID, sessionentry.ContextEntry{
		ID:         inputEntryID,
		RunID:      runID.String(),
		TurnID:     identity.NewTurnID(runID, 1).String(),
		Role:       role.User,
		AuthorKind: sessionentry.AuthorUser,
		Content:    []sessionentry.ContentBlock{sessionentry.TextBlock{Text: input.Prompt}},
		CreatedAt:  at,
	})
	if err != nil {
		return result, false, err
	}
	result = agentrun.NewRunRecord(runID, session.ID, session.MainAgentID(), input.ID, inputEntryID, config, at)
	result.AgentName = definition.Name
	result.BranchID = input.BranchID
	if _, err = s.records.SaveRun(ctx, result); err != nil {
		return agentrun.RunRecord{}, false, err
	}
	if !inserted {
		return result, false, nil
	}
	// Run 保存成功后发布输入；通知独立于提交后 Hook，也不依赖 Loop 启动。
	if s.emitter != nil {
		s.emitter.Emit(coreevent.Event{
			Source: coreevent.EventSource{
				SessionID: result.SessionID,
				AgentID:   result.AgentID,
				BranchID:  result.BranchID,
				BindingID: bindingID,
				RunID:     result.ID,
			},
			Payload: coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{*stored}},
		})
	}
	if hookErr := s.hookDispatcher.UserInputCommitted(context.WithoutCancel(ctx), hook.EntryCommitted{
		Identity: hook.Identity{
			SessionID:    result.SessionID,
			AgentID:      result.AgentID,
			SourceTaskID: result.SourceTaskID,
			RunID:        result.ID,
		},
		Entry: *stored,
	}); hookErr != nil {
		s.logger.Errorf("user message committed hook failed: %v", hookErr)
	}
	return result, false, nil
}
