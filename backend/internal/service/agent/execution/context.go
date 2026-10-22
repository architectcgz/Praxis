package execution

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/loop"
	"praxis/internal/repository"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

func (s *Service) loadContextEntries(ctx context.Context, sessionID identity.SessionID) ([]sessionmodel.SharedFact, error) {
	var entries []sessionmodel.SharedFact
	for afterID := ""; ; {
		page, err := s.contexts.List(ctx, sessionID, afterID, 512)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return entries, nil
		}
		entries = append(entries, page...)
		afterID = page[len(page)-1].ID.String()
	}
}

// PrepareLoop 在工具回执补齐后校验当前执行归属、判断完成证据并构建 Session 内存上下文。
// completed 为 true 时由 Coordinator 直接结算，不能再次进入 Loop。
func (s *Service) PrepareLoop(ctx context.Context, executionID identity.AgentExecutionID) (executionInput loop.Input, completed bool, err error) {
	if ctx == nil {
		return loop.Input{}, false, errors.New("execution preparation context is required")
	}
	err = func(txCtx context.Context) error {
		task, err := s.records.Get(txCtx, executionID)
		if err != nil {
			return err
		}
		agent, err := s.agents.Get(txCtx, task.AgentID)
		if err != nil {
			return err
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, task.SessionID); err != nil {
			return err
		}
		if !task.Active() || agent.CurrentExecutionID != task.ID {
			return fault.ErrRequestConflict
		}
		// 完成证据由仓储从最后一条 Assistant 派生；消息落盘后重启不再请求模型。
		if task.Progress.FinalAssistantID != "" {
			completed = true
			return nil
		}
		stream, err := s.entries.List(txCtx, repository.EntryQuery{Stream: agentservice.EntryStream(agent), ForContext: true})
		if err != nil {
			return err
		}
		var shared []sessionmodel.SharedFact
		if agent.CanReadSessionContext() {
			shared, err = s.loadContextEntries(txCtx, agent.SessionID)
			if err != nil {
				return err
			}
		}
		content, err := contextContent(task.Config.SystemPrompt, task.InputEntryID, shared, stream.Entries)
		if err != nil {
			return err
		}
		identity := hook.Identity{
			OperationID:  hook.NewOperationID(),
			SourceTaskID: task.SourceTaskID,
			SessionID:    task.SessionID,
			AgentID:      task.AgentID,
			ExecutionID:  task.ID,
		}
		limits := appcontext.Limits{ContextWindow: task.Config.Model.ContextWindow, MaxOutputTokens: task.Config.Model.MaxOutputTokens}
		content, err = s.hookDispatcher.BeforeContextBuild(txCtx, hook.ContextBuild{Identity: identity, Content: content, Limits: limits})
		if err != nil {
			return err
		}
		built, err := (appcontext.ContextBuilder{}).Build(content, limits)
		if hookErr := s.hookDispatcher.AfterContextBuild(context.WithoutCancel(txCtx), hook.ContextBuilt{Identity: identity, Context: built, Err: err}); hookErr != nil {
			s.logger.Errorf("after context build hook failed: %v", hookErr)
		}
		if err != nil {
			return err
		}
		sessionContext, err := s.entries.SetContext(txCtx, agentservice.EntryStream(agent), built)
		if err != nil {
			return err
		}
		executionInput = loop.Input{
			ID:           task.ID,
			SourceTaskID: task.SourceTaskID,
			SessionID:    task.SessionID,
			AgentID:      task.AgentID,
			RequestID:    task.RequestID,
			Config:       loop.Config{Model: task.Config.Model.Clone()},
			Context:      sessionContext,
		}
		return nil
	}(ctx)
	if err != nil {
		return loop.Input{}, false, err
	}
	return executionInput, completed, nil
}
