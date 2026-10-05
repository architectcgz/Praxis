// Package control owns durable Pause and Close command handling.
package control

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"

	"context"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/repository"
	"praxis/internal/system"
)

// RuntimeCancellation sends a durable control request to a process-local
// runtime after the transaction that recorded it has committed.
type RuntimeCancellation interface {
	Cancel(context.Context, contracts.AgentID, contracts.TurnID, turnmodel.TurnOutcome) (bool, error)
}

// TurnSettler 在人工控制找不到进程内 runtime 时完成持久化结算。
type TurnSettler interface {
	SettleRuntimeTurn(context.Context, contracts.TurnID, turnmodel.TurnOutcome, contracts.TurnFailureCode, string) error
}

// Config 包含控制类应用服务所需的依赖。
type Config struct {
	Transactions repository.TxRunner
	Agents       repository.SessionAgentRepository
	Controls     repository.AgentControlCommandRepository
	Canceller    RuntimeCancellation
	Settler      TurnSettler
	Clock        system.Clock
}

// Service owns the control-request transaction and post-commit cancellation.
type Service struct {
	tx        repository.TxRunner
	agents    repository.SessionAgentRepository
	controls  repository.AgentControlCommandRepository
	canceller RuntimeCancellation
	settler   TurnSettler
	clock     system.Clock
}

// Params identifies one Pause or Close command.
type Params struct {
	CommandID contracts.AgentControlCommandID
	AgentID   contracts.AgentID
}

// Result returns the durable control command and cancellation outcome.
type Result struct {
	Command           workflowmodel.AgentControlCommand
	ExistingCommand   bool
	CancellationError string
}

// NewService creates the control application service.
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"agents":       config.Agents,
		"controls":     config.Controls,
		"settler":      config.Settler,
	} {
		if value == nil {
			return nil, fmt.Errorf("control service %s is required", name)
		}
	}
	return &Service{
		tx:        config.Transactions,
		agents:    config.Agents,
		controls:  config.Controls,
		canceller: config.Canceller,
		settler:   config.Settler,
		clock:     system.ClockOrDefault(config.Clock),
	}, nil
}

// PauseAgent records a durable pause command before signaling cancellation.
func (s *Service) PauseAgent(ctx context.Context, params Params) (Result, error) {
	return s.apply(ctx, params, workflowmodel.AgentControlPause)
}

// CloseAgent records a durable close command before signaling cancellation.
func (s *Service) CloseAgent(ctx context.Context, params Params) (Result, error) {
	return s.apply(ctx, params, workflowmodel.AgentControlClose)
}

// apply records Pause or Close before signaling runtime cancellation. A caller
// timeout cannot retract the committed control command.
func (s *Service) apply(ctx context.Context, params Params, kind workflowmodel.AgentControlKind) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("control command context is required")
	}
	params.CommandID = contracts.AgentControlCommandID(strings.TrimSpace(params.CommandID.String()))
	params.AgentID = contracts.AgentID(strings.TrimSpace(params.AgentID.String()))
	if params.CommandID == "" || params.AgentID == "" ||
		(kind != workflowmodel.AgentControlPause && kind != workflowmodel.AgentControlClose) {
		return Result{}, contracts.New(contracts.InvalidRequest, "")
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.controls.Get(txCtx, params.CommandID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.Kind != kind {
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
		at := s.clock.Now()
		control, err := workflowmodel.NewAgentControlCommand(params.CommandID, agent.ID, agent.CurrentTurnID, kind, at)
		if err != nil {
			return err
		}
		if agent.State == agentmodel.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == agentmodel.AgentPausing {
			// 人工重试可以接管上一次未完成的进程内取消。
		} else if kind == workflowmodel.AgentControlClose {
			if err := agent.Close(at); err != nil {
				return err
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
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
	if err := s.settler.SettleRuntimeTurn(
		context.WithoutCancel(ctx),
		result.Command.TargetTurnID,
		cancellationOutcome(result.Command.Kind),
		"",
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
