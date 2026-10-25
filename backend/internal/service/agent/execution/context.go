package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
)

// PrepareLoop 只使用本次 Run 构建模型投影，不重新读取持久化历史。
// completed 为 true 时由 Coordinator 直接结算，不能再次进入 Loop。
func (s *Service) PrepareLoop(ctx context.Context, run *agentexecution.Run) (executionInput loop.Input, completed bool, err error) {
	err = func(txCtx context.Context) error {
		task := run.Snapshot()
		if !task.Active() {
			return errorcode.ErrRequestConflict
		}
		// 完成证据由仓储从最后一条 Assistant 派生；消息落盘后重启不再请求模型。
		if task.Progress.FinalAssistantID != "" {
			completed = true
			return nil
		}
		content, err := contextContent(task.Config.SystemPrompt, task.InputEntryID, run.ContextEntries())
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
		run.Context = built
		executionInput = loop.Input{
			Run:          run,
			ID:           task.ID,
			SourceTaskID: task.SourceTaskID,
			SessionID:    task.SessionID,
			AgentID:      task.AgentID,
			RequestID:    task.RequestID,
			Config:       loop.Config{Model: task.Config.Model.Clone()},
			Context:      &run.Context,
		}
		return nil
	}(ctx)
	if err != nil {
		return loop.Input{}, false, err
	}
	return executionInput, completed, nil
}
