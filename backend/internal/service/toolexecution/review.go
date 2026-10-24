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

// PrepareReview 在模型调用前读取输入、预留请求序号并保存用量归属。
func (s *Service) PrepareReview(ctx context.Context, call sessionentry.ToolCall, workspacePath string) (executionmodel.ReviewRequest, error) {
	task, err := s.deps.Tasks.Get(ctx, call.ExecutionID)
	if err != nil {
		return executionmodel.ReviewRequest{}, err
	}
	if task.Status != agentexecution.ExecutionRunning {
		return executionmodel.ReviewRequest{}, errorcode.ErrRequestConflict
	}
	input, err := s.deps.Entries.Get(ctx, task.AgentID, task.InputEntryID)
	if err != nil {
		return executionmodel.ReviewRequest{}, err
	}
	userTask := input.TextContent()
	if userTask == "" {
		return executionmodel.ReviewRequest{}, errors.New("risk review cannot locate the task input")
	}
	sequence := max(uint64(1), task.Progress.NextSequence)
	task.Progress.NextSequence, task.Progress.ModelRequests = sequence+1, task.Progress.ModelRequests+1
	if err := s.deps.Tasks.Save(ctx, task); err != nil {
		return executionmodel.ReviewRequest{}, err
	}
	return executionmodel.ReviewRequest{
		Model:         task.Config.Model.Clone(),
		UserTask:      userTask,
		WorkspacePath: workspacePath,
		Attempt: executionmodel.RiskReview{
			TurnID:     identity.NewTurnID(task.ID, sequence),
			ProviderID: task.Config.Model.ProviderID,
			ModelID:    task.Config.Model.ModelID,
			StartedAt:  s.clock.Now().UTC(),
		},
	}, nil
}
