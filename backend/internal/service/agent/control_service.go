package agent

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// ControlParams 标识控制目标；必须使用观察到的 Execution ID，延迟命令不能影响下一项执行。
type ControlInput struct {
	CommandID         identity.AgentControlCommandID
	AgentID           identity.AgentID
	TargetExecutionID identity.AgentExecutionID
}

// ControlResult 返回持久化控制命令；信号送达不代表 runtime 已完成结算。
type ControlResult struct {
	Command           agentmodel.AgentControlCommand
	ExistingCommand   bool
	CancellationError string
}

// PauseAgent 先提交暂停意图，再通知执行控制能力；重复请求保持幂等。
func (s *Service) PauseAgent(ctx context.Context, params ControlInput) (ControlResult, error) {
	return s.applyControl(ctx, params, agentmodel.AgentControlPause)
}

// StopAgent 停止明确指定的活动 Execution，保留预约任务，Agent 中断后可基于历史接收新输入。
func (s *Service) StopAgent(ctx context.Context, params ControlInput) (ControlResult, error) {
	return s.applyControl(ctx, params, agentmodel.AgentControlCancel)
}

// 控制意图先提交，Execution 的条件写入负责仲裁完成与暂停、停止的竞争。
func (s *Service) applyControl(ctx context.Context, params ControlInput, kind agentmodel.AgentControlKind) (ControlResult, error) {
	if ctx == nil {
		return ControlResult{}, errors.New("control command context is required")
	}
	var result ControlResult
	err := func(txCtx context.Context) error {
		existing, err := s.controls.Get(txCtx, params.CommandID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.TargetExecutionID != params.TargetExecutionID || existing.Kind != kind {
				return fault.New(fault.InvalidRequest, "")
			}
			result = ControlResult{Command: existing, ExistingCommand: true}
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		task, err := s.executionRecords.Get(txCtx, params.TargetExecutionID)
		if err != nil {
			return err
		}
		if task.AgentID != agent.ID {
			return fault.New(fault.InvalidRequest, "")
		}
		at := s.clock.Now()
		control := agentmodel.NewAgentControlCommand(params.CommandID, agent.ID, task.ID, kind, at)
		if task.Active() && (agent.CurrentExecutionID != task.ID || agent.State != agentmodel.AgentExecuting && agent.State != agentmodel.AgentPausing) {
			return fault.New(agentmodel.UnavailableCode, "")
		}
		if err := s.controls.Save(txCtx, control); err != nil {
			return err
		}
		result.Command, err = s.controls.Get(txCtx, control.ID)
		return err
	}(ctx)
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
		s.executions.Pause(command.AgentID, command.TargetExecutionID)
	} else {
		s.executions.Stop(command.AgentID, command.TargetExecutionID)
	}
	// loop 已返回时无需再发信号，结算负责仲裁已提交的控制命令。
	return result
}
