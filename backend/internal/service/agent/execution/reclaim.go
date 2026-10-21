package execution

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/fault"
)

// ReclaimExecution 为同一个遗留 Execution 重新建立执行身份；冻结输入和已使用预算不变。
func (s *Service) ReclaimExecution(ctx context.Context, selected agentmodel.Execution) (agentmodel.Execution, error) {
	var task agentmodel.Execution
	created := false
	err := func(txCtx context.Context) error {
		var err error
		task, err = s.records.Get(txCtx, selected.ID)
		if err != nil {
			return err
		}
		if !task.Active() || task.AgentID != selected.AgentID {
			return fault.ErrRequestConflict
		}
		agent, err := s.agents.Get(txCtx, task.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentExecutionID != task.ID {
			return fault.ErrRequestConflict
		}
		_, created, err = s.executions.Reserve(ctx, task.AgentID, task.ID)
		if err != nil {
			return err
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, task.AgentID, 0)
		if err != nil {
			return err
		}
		for _, control := range controls {
			if control.TargetExecutionID != task.ID {
				continue
			}
			if control.Kind == agentmodel.AgentControlCancel {
				s.executions.Stop(task.AgentID, task.ID)
			} else {
				s.executions.Pause(task.AgentID, task.ID)
			}
		}
		return nil
	}(ctx)
	if err != nil && created {
		s.executions.Release(selected.AgentID, selected.ID)
	}
	return task, err
}
