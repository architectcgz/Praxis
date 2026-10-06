// Package control 负责持久化暂停或取消命令，并在提交后通知 runtime。
package control

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"

	"context"
	"errors"
	"fmt"

	"praxis/internal/repository"
	"praxis/internal/system"
)

// RuntimeCancellation 只转发已提交的控制命令，按 Turn ID 取消对应的进程内执行。
type RuntimeCancellation interface {
	Cancel(context.Context, contracts.AgentID, contracts.TurnID, turnmodel.TurnOutcome) (bool, error)
}

// TurnEnder 在控制命令找不到进程内 runtime 时持久化最终结果。
type TurnEnder interface {
	EndRuntimeTurn(context.Context, contracts.TurnID, turnmodel.TurnOutcome, contracts.TurnFailureCode, string) error
}

// Config 包含控制类应用服务所需的依赖。
type Config struct {
	Transactions repository.TxRunner
	Agents       repository.SessionAgentRepository
	Turns        repository.TurnRepository
	Controls     repository.AgentControlCommandRepository
	Canceller    RuntimeCancellation
	Ender        TurnEnder
	Clock        system.Clock
}

// Service 管理控制命令事务与提交后的取消通知，不关闭 Agent。
type Service struct {
	tx        repository.TxRunner
	agents    repository.SessionAgentRepository
	turns     repository.TurnRepository
	controls  repository.AgentControlCommandRepository
	canceller RuntimeCancellation
	ender     TurnEnder
	clock     system.Clock
}

// Params 标识暂停或取消的目标回合；必须使用前端观察到的 Turn ID，不能隐式选择下一轮。
type Params struct {
	CommandID    contracts.AgentControlCommandID
	AgentID      contracts.AgentID
	TargetTurnID contracts.TurnID
}

// Result 返回已持久化的命令；CancellationError 表示命令已提交但执行通知失败。
type Result struct {
	Command           workflowmodel.AgentControlCommand
	ExistingCommand   bool
	CancellationError string
}

// NewService 创建控制服务；缺少持久化或结束依赖时返回错误。
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"agents":       config.Agents,
		"turns":        config.Turns,
		"controls":     config.Controls,
		"ender":        config.Ender,
	} {
		if value == nil {
			return nil, fmt.Errorf("control service %s is required", name)
		}
	}
	return &Service{
		tx:        config.Transactions,
		agents:    config.Agents,
		turns:     config.Turns,
		controls:  config.Controls,
		canceller: config.Canceller,
		ender:     config.Ender,
		clock:     system.ClockOrDefault(config.Clock),
	}, nil
}

// PauseAgent 先保存暂停指定回合的命令，再取消运行；重复命令保持幂等。
func (s *Service) PauseAgent(ctx context.Context, params Params) (Result, error) {
	return s.apply(ctx, params, workflowmodel.AgentControlPause)
}

// CancelTurn 只取消指定回合，Agent 中断后仍可接收新的输入。
func (s *Service) CancelTurn(ctx context.Context, params Params) (Result, error) {
	return s.apply(ctx, params, workflowmodel.AgentControlCancel)
}

// apply 先提交控制命令；调用方超时不能撤销已提交命令，旧回合请求不能影响下一轮。
func (s *Service) apply(ctx context.Context, params Params, kind workflowmodel.AgentControlKind) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("control command context is required")
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.controls.Get(txCtx, params.CommandID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.TargetTurnID != params.TargetTurnID || existing.Kind != kind {
				return contracts.New(contracts.InvalidRequest, "")
			}
			result = Result{Command: existing, ExistingCommand: true}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		turn, err := s.turns.Get(txCtx, params.TargetTurnID)
		if err != nil {
			return err
		}
		if turn.AgentID != agent.ID {
			return contracts.New(contracts.InvalidRequest, "")
		}
		at := s.clock.Now()
		control, err := workflowmodel.NewAgentControlCommand(params.CommandID, agent.ID, turn.ID, kind, at)
		if err != nil {
			return err
		}
		if turn.Status == turnmodel.TurnEnded {
			if err := control.MarkApplied(at); err != nil {
				return err
			}
		} else if agent.CurrentTurnID != turn.ID {
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
		return Result{}, err
	}
	if result.Command.Status == workflowmodel.AgentControlApplied {
		return result, nil
	}
	if s.canceller != nil {
		cancelled, err := s.canceller.Cancel(
			context.WithoutCancel(ctx),
			result.Command.AgentID,
			result.Command.TargetTurnID,
			cancellationOutcome(result.Command.Kind),
		)
		if err != nil {
			result.CancellationError = err.Error()
			return result, nil
		}
		if cancelled {
			return result, nil
		}
	}
	if err := s.ender.EndRuntimeTurn(
		context.WithoutCancel(ctx),
		result.Command.TargetTurnID,
		cancellationOutcome(result.Command.Kind),
		contracts.TurnFailureRequestCanceled,
		"",
	); err != nil {
		result.CancellationError = err.Error()
		return result, nil
	}
	command, err := s.controls.Get(ctx, result.Command.ID)
	if err != nil {
		result.CancellationError = err.Error()
		return result, nil
	}
	result.Command = command
	return result, nil
}

func cancellationOutcome(kind workflowmodel.AgentControlKind) turnmodel.TurnOutcome {
	if kind == workflowmodel.AgentControlPause {
		return turnmodel.TurnPaused
	}
	return turnmodel.TurnInterrupted
}
