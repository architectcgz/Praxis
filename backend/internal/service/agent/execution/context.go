package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
)

// PrepareLoop 检查完成证据，构建独立模型上下文，并绑定本次执行使用的业务能力。
// completed 为 true 时由 Coordinator 直接结算，不能再次进入 Loop。
func (s *Service) PrepareLoop(ctx context.Context, run *agentexecution.Run) (loop.Input, loop.ExecutionDependencies, bool, error) {
	task := run.Snapshot()
	if !task.Active() {
		return loop.Input{}, loop.ExecutionDependencies{}, false, errorcode.ErrRequestConflict
	}
	// 完成证据由已接纳的 Assistant 派生，恢复后不重复请求模型。
	if task.Progress.FinalAssistantID != "" {
		return loop.Input{}, loop.ExecutionDependencies{}, true, nil
	}
	built, err := s.buildContext(ctx, run)
	if err != nil {
		return loop.Input{}, loop.ExecutionDependencies{}, false, err
	}
	input := loop.Input{
		ID:           task.ID,
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		RequestID:    task.RequestID,
		Model:        task.Config.Model.Clone(),
		Context:      built,
	}
	bound := &loopExecution{
		service: s,
		run:     run,
	}
	deps := loop.ExecutionDependencies{
		Execution: bound,
		History:   bound,
		Timing:    run.Timing,
	}
	return input, deps, false, nil
}

// buildContext 从会话或子执行已维护的上下文构建本轮模型请求。
// 当前输入保护和提示词只属于本轮，不从完整历史重新筛选条目。
func (s *Service) buildContext(ctx context.Context, run *agentexecution.Run) (appcontext.ModelContext, error) {
	task := run.Snapshot()
	if !task.Active() {
		return appcontext.ModelContext{}, errorcode.ErrRequestConflict
	}
	var entries []sessionentry.ContextEntry
	if run.Main() {
		value, err := s.history.ContextEntries(ctx, task.SessionID)
		if err != nil {
			return appcontext.ModelContext{}, loop.NewFailure(loop.FailureStorage, err)
		}
		entries = value
	} else {
		value, err := run.ContextEntries()
		if err != nil {
			return appcontext.ModelContext{}, loop.NewFailure(loop.FailureContract, err)
		}
		entries = value
	}
	content, err := appcontext.ContentFromEntries(task.Config.SystemPrompt, task.InputEntryID, entries)
	if err != nil {
		return appcontext.ModelContext{}, err
	}
	identity := hook.Identity{
		OperationID:  hook.NewOperationID(),
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		ExecutionID:  task.ID,
	}
	limits := appcontext.Limits{
		ContextWindow:   task.Config.Model.ContextWindow,
		MaxOutputTokens: task.Config.Model.MaxOutputTokens,
	}
	content, err = s.hookDispatcher.BeforeContextBuild(ctx, hook.ContextBuild{
		Identity: identity,
		Content:  content,
		Limits:   limits,
	})
	if err != nil {
		return appcontext.ModelContext{}, err
	}
	built, err := (appcontext.ContextBuilder{}).Build(content, limits)
	if hookErr := s.hookDispatcher.AfterContextBuild(context.WithoutCancel(ctx), hook.ContextBuilt{
		Identity: identity,
		Context:  built,
		Err:      err,
	}); hookErr != nil {
		s.logger.Errorf("after context build hook failed: %v", hookErr)
	}
	return built, err
}
