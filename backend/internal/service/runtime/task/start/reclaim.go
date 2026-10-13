package start

import (
	"context"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"
)

// ReclaimTask 为同一个遗留 Task 重新建立执行身份；冻结输入和已使用预算不变。
func (s *Service) ReclaimTask(ctx context.Context, selected taskmodel.Task) (taskmodel.Task, error) {
	var task taskmodel.Task
	created := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		task, err = s.tasks.Get(txCtx, selected.ID)
		if err != nil {
			return err
		}
		if !task.Active() || task.AgentID != selected.AgentID {
			return contracts.ErrRequestConflict
		}
		agent, err := s.agents.Get(txCtx, task.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentTaskID != task.ID {
			return contracts.ErrRequestConflict
		}
		_, created, err = s.executions.Start(ctx, task.AgentID, task.ID)
		if err != nil {
			return err
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, task.AgentID, 0)
		if err != nil {
			return err
		}
		for _, control := range controls {
			if control.TargetTaskID != task.ID {
				continue
			}
			if control.Kind == agentmodel.AgentControlCancel {
				s.executions.Stop(task.AgentID, task.ID)
			} else {
				s.executions.Pause(task.AgentID, task.ID)
			}
		}
		return nil
	})
	if err != nil && created {
		s.executions.End(selected.AgentID, selected.ID)
	}
	return task, err
}
