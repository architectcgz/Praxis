package execution

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
)

// PrepareLoop 在工具回执补齐后校验执行归属，并从 Agent 自身历史构建本次唯一的模型上下文。
// Session 直接使用 Main 的上下文；Subagent 使用自身历史，不额外加载 Session 内容。
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
			return errorcode.ErrRequestConflict
		}
		// 完成证据由仓储从最后一条 Assistant 派生；消息落盘后重启不再请求模型。
		if task.Progress.FinalAssistantID != "" {
			completed = true
			return nil
		}
		page, err := s.entries.List(txCtx, repository.EntryQuery{AgentID: agent.ID, ForContext: true})
		if err != nil {
			return err
		}
		content, err := contextContent(task.Config.SystemPrompt, task.InputEntryID, page.Entries)
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
		executionInput = loop.Input{
			ID:           task.ID,
			SourceTaskID: task.SourceTaskID,
			SessionID:    task.SessionID,
			AgentID:      task.AgentID,
			RequestID:    task.RequestID,
			Config:       loop.Config{Model: task.Config.Model.Clone()},
			Context:      &built,
		}
		return nil
	}(ctx)
	if err != nil {
		return loop.Input{}, false, err
	}
	return executionInput, completed, nil
}
