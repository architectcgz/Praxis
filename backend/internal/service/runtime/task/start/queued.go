package start

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// BuildTask 为 runtime 选中的队首 Task 构建 Context，保留原有 Task ID。
// Agent 忙碌、已被领取或不是队首时返回 false；任何构建失败都回滚，不消费待处理输入。
func (s *Service) BuildTask(ctx context.Context, selected taskmodel.Task) (taskmodel.Task, bool, error) {
	if ctx == nil {
		return taskmodel.Task{}, false, errors.New("build task context is required")
	}
	var built taskmodel.Task
	var admitted bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := s.agents.Get(txCtx, selected.AgentID)
		if err != nil {
			return err
		}
		if !agent.CanReadSessionContext() {
			return contracts.New(contracts.InvalidRequest, "queue requires the session main agent")
		}
		task, err := s.tasks.FindNextPendingByAgent(txCtx, agent.ID)
		if errors.Is(err, contracts.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if task.ID != selected.ID {
			return nil
		}
		if !agent.State.Startable() {
			return nil
		}
		active, err := s.tasks.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active != 0 {
			return nil
		}
		input, err := s.buildInputSnapshot(txCtx, agent, task.ProviderID, task.ModelID, task.ReasoningLevel, "", "input:"+task.RequestID.String())
		if err != nil {
			return err
		}
		at := s.clock.Now()
		if err := task.Prepare(input); err != nil {
			return err
		}
		if err := agent.Start(task.ID, at); err != nil {
			return err
		}
		_, admitted, err = s.executions.Start(ctx, agent.ID, task.ID)
		if err != nil {
			return err
		}
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		built = task
		return nil
	})
	if err != nil && admitted {
		s.executions.End(selected.AgentID, selected.ID)
	}
	if err != nil {
		return taskmodel.Task{}, false, err
	}
	return built, built.ID != "", nil
}
