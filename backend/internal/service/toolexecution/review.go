package toolexecution

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// PrepareReview 与主模型、摘要共用 Runtime 中的序号，Main 预留失败时不发起审核请求。
func (s *Service) PrepareReview(ctx context.Context, callRuntime *agentexecution.Runtime, call sessionentry.ToolCall, workspacePath string) (executionmodel.ReviewRequest, error) {
	task := callRuntime.Snapshot()
	if task.Status != agentexecution.ExecutionRunning || call.ExecutionID != task.ID {
		return executionmodel.ReviewRequest{}, errorcode.ErrRequestConflict
	}
	input, found := callRuntime.Entry(task.InputEntryID)
	if !found || input.TextContent() == "" {
		return executionmodel.ReviewRequest{}, errors.New("risk review cannot locate the task input")
	}
	sequence := max(uint64(1), task.Progress.NextSequence)
	if err := callRuntime.Update(func(record *agentexecution.Execution) error {
		record.Progress.NextSequence, record.Progress.ModelRequests = sequence+1, record.Progress.ModelRequests+1
		if callRuntime.Main() {
			return s.deps.Tasks.Save(ctx, *record)
		}
		return ctx.Err()
	}); err != nil {
		return executionmodel.ReviewRequest{}, err
	}
	return executionmodel.ReviewRequest{
		Runtime:       callRuntime,
		Model:         task.Config.Model.Clone(),
		UserTask:      input.TextContent(),
		WorkspacePath: workspacePath,
		Attempt: executionmodel.RiskReview{
			TurnID:     identity.NewTurnID(task.ID, sequence),
			ProviderID: task.Config.Model.ProviderID,
			ModelID:    task.Config.Model.ModelID,
			StartedAt:  s.clock.Now().UTC(),
		},
	}, nil
}
