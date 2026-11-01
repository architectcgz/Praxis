package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
)

// PrepareLoop 绑定请求投影和本次执行能力，completed 为 true 时不得再次执行用户任务。
func (s *Service) PrepareLoop(ctx context.Context, run *agentexecution.Run) (loop.Input, loop.ExecutionDependencies, bool, error) {
	task := run.Snapshot()
	if !task.Active() {
		return loop.Input{}, loop.ExecutionDependencies{}, false, errorcode.ErrRequestConflict
	}
	// 完成证据由已接纳的 Assistant 派生，恢复后不重复请求模型。
	content, err := s.contextContent(ctx, run)
	if err != nil {
		return loop.Input{}, loop.ExecutionDependencies{}, false, err
	}
	input := loop.Input{
		ID:           task.ID,
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		BranchID:     task.BranchID,
		BindingID:    run.BindingID,
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
		Entries:   bound,
		Timing:    run.Timing,
	}
	return input, deps, task.Progress.FinalAssistantID != "", nil
}

// contextContent 只读取本次 Run 的 Context，当前输入保护不回写共享 Entry。
func (s *Service) contextContent(ctx context.Context, run *agentexecution.Run) (appcontext.Content, error) {
	task := run.Snapshot()
	if !task.Active() {
		return appcontext.Content{}, loop.NewFailure(loop.FailureContract, errorcode.ErrRequestConflict)
	}
	if err := ctx.Err(); err != nil {
		return appcontext.Content{}, err
	}
	return appcontext.ContentFromEntries(task.Config.SystemPrompt, task.InputEntryID, run.ContextEntries()), nil
}
