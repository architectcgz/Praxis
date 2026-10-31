package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/loop"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
)

// PrepareLoop 检查完成证据，准备基础上下文副本，并绑定本次执行使用的业务能力。
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
	content, err := s.contextContent(ctx, run)
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
		Context:      content,
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

// contextContent 从会话或子执行取得未经过 Hook 变换的基础上下文副本。
// 当前输入保护和提示词只属于本轮；压缩和模型输入构建由 Loop 编排。
func (s *Service) contextContent(ctx context.Context, run *agentexecution.Run) (appcontext.Content, error) {
	task := run.Snapshot()
	if !task.Active() {
		return appcontext.Content{}, loop.NewFailure(loop.FailureContract, errorcode.ErrRequestConflict)
	}
	var entries []sessionentry.ContextEntry
	if run.Main() {
		value, err := s.history.ContextEntries(ctx, task.SessionID)
		if err != nil {
			return appcontext.Content{}, loop.NewFailure(loop.FailureStorage, err)
		}
		entries = value
	} else {
		value, err := run.ContextEntries()
		if err != nil {
			return appcontext.Content{}, loop.NewFailure(loop.FailureContract, err)
		}
		entries = value
	}
	content, err := appcontext.ContentFromEntries(task.Config.SystemPrompt, task.InputEntryID, entries)
	if err != nil {
		return appcontext.Content{}, loop.NewFailure(loop.FailureContract, err)
	}
	return content, nil
}
