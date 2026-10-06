package agent

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"

	"context"
	"errors"
)

// ControlParams 标识控制目标；必须使用观察到的 Task ID，延迟命令不能影响下一项执行。
type ControlParams struct {
	CommandID    contracts.AgentControlCommandID
	AgentID      contracts.AgentID
	TargetTaskID contracts.TaskID
}

// ControlResult 返回持久化控制命令；信号送达不代表 runtime 已完成结算。
type ControlResult struct {
	Command           agentmodel.AgentControlCommand
	ExistingCommand   bool
	CancellationError string
}

// PauseAgent 提交暂停意图后调用 core/agent 暂停执行；重复请求保持幂等。
func (s *Service) PauseAgent(ctx context.Context, params ControlParams) (ControlResult, error) {
	return s.applyControl(ctx, params, agentmodel.AgentControlPause)
}

// StopAgent 停止明确指定的活动 Task，保留预约任务，Agent 中断后可基于历史接收新输入。
func (s *Service) StopAgent(ctx context.Context, params ControlParams) (ControlResult, error) {
	return s.applyControl(ctx, params, agentmodel.AgentControlCancel)
}

// 控制意图必须先提交，结束事务才可以仲裁正常完成与暂停、停止的竞争。
func (s *Service) applyControl(ctx context.Context, params ControlParams, kind agentmodel.AgentControlKind) (ControlResult, error) {
	if ctx == nil {
		return ControlResult{}, errors.New("control command context is required")
	}
	var result ControlResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.controls.Get(txCtx, params.CommandID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.TargetTaskID != params.TargetTaskID || existing.Kind != kind {
				return contracts.New(contracts.InvalidRequest, "")
			}
			result = ControlResult{Command: existing, ExistingCommand: true}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		task, err := s.tasks.Get(txCtx, params.TargetTaskID)
		if err != nil {
			return err
		}
		if task.AgentID != agent.ID {
			return contracts.New(contracts.InvalidRequest, "")
		}
		at := s.clock.Now()
		control, err := agentmodel.NewAgentControlCommand(params.CommandID, agent.ID, task.ID, kind, at)
		if err != nil {
			return err
		}
		if task.Status == taskmodel.TaskEnded {
			if err := control.MarkApplied(at); err != nil {
				return err
			}
		} else if agent.CurrentTaskID != task.ID {
			return contracts.New(contracts.AgentUnavailable, "")
		} else if agent.State == agentmodel.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == agentmodel.AgentPausing {
			// 人工重试可以接管上一次未完成的进程内取消。
		} else {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		if err := s.controls.Save(txCtx, control); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Command = control
		return nil
	})
	if err != nil {
		return ControlResult{}, err
	}
	return s.signalControl(result), nil
}

func (s *Service) signalControl(result ControlResult) ControlResult {
	if result.Command.Status == agentmodel.AgentControlApplied {
		return result
	}
	command := result.Command
	if command.Kind == agentmodel.AgentControlPause {
		s.executions.Pause(command.AgentID, command.TargetTaskID)
	} else {
		s.executions.Stop(command.AgentID, command.TargetTaskID)
	}
	// loop 已返回时无需再发信号，结束事务负责应用已提交的控制命令。
	return result
}
