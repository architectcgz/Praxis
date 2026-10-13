// Package turn 负责 loop 迭代的持久化开始与结算，不拥有 Provider 与工具调用的执行。
package turn

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/repository"
	"praxis/internal/system"
)

type Config struct {
	Transactions repository.TxRunner
	Turns        repository.TurnRepository
	Clock        system.Clock
}

// Service 持久化迭代生命周期；身份由 Task 与迭代序号推导，因此重放保持幂等。
type Service struct {
	tx    repository.TxRunner
	turns repository.TurnRepository
	clock system.Clock
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"turns":        config.Turns,
	} {
		if value == nil {
			return nil, fmt.Errorf("turn service %s is required", name)
		}
	}
	return &Service{
		tx:    config.Transactions,
		turns: config.Turns,
		clock: system.ClockOrDefault(config.Clock),
	}, nil
}

// RecordStart 幂等记录 running 迭代；同一 Task 的同一序号返回已存在记录，不覆盖历史事实。
func (s *Service) RecordStart(ctx context.Context, params contracts.TurnExecutionContext) (turnmodel.Turn, error) {
	if ctx == nil {
		return turnmodel.Turn{}, errors.New("turn record start context is required")
	}
	id, err := turnmodel.NewTurnID(params.TaskID, params.Sequence)
	if err != nil {
		return turnmodel.Turn{}, err
	}
	var value turnmodel.Turn
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.turns.Get(txCtx, id)
		if err == nil {
			value = existing
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		value, err = turnmodel.NewRunning(
			id, params.TaskID, params.SessionID, params.AgentID, params.Sequence, s.clock.Now(),
		)
		if err != nil {
			return err
		}
		return s.turns.Save(txCtx, value)
	})
	if err != nil {
		return turnmodel.Turn{}, err
	}
	return value, nil
}

// RecordEnd 记录 running 迭代的终态；已结算的迭代直接返回，不重复写入终态。
func (s *Service) RecordEnd(
	ctx context.Context,
	id contracts.TurnID,
	outcome turnmodel.TurnOutcome,
	code contracts.TaskFailureCode,
	message string,
) error {
	if ctx == nil {
		return errors.New("turn record end context is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		value, err := s.turns.Get(txCtx, id)
		if err != nil {
			return err
		}
		if value.Status == turnmodel.TurnEnded {
			return nil
		}
		if err := value.End(outcome, code, message, s.clock.Now()); err != nil {
			return err
		}
		return s.turns.Save(txCtx, value)
	})
}

// Execute 负责一次 Turn 的开始、业务执行和终态结算；业务执行失败仍返回原始结果，结算失败优先返回存储错误。
// 业务执行使用原始 Context，结算使用脱离取消的 Context，避免取消后遗留 running Turn。
func (s *Service) Execute(
	ctx context.Context,
	params contracts.TurnExecutionContext,
	execute func(context.Context, contracts.TurnID) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error),
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	if ctx == nil {
		return taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, errors.New("turn execution context is required")
	}
	if execute == nil {
		return taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, errors.New("turn execution function is required")
	}
	turn, err := s.RecordStart(ctx, params)
	if err != nil {
		return taskmodel.TaskFailed, contracts.TaskFailureStorage, fmt.Errorf("record turn start: %w", err)
	}
	outcome, code, executeErr := execute(ctx, turn.ID)
	turnOutcome, failureCode, message := settlement(ctx, outcome, code, executeErr)
	if err := s.RecordEnd(context.WithoutCancel(ctx), turn.ID, turnOutcome, failureCode, message); err != nil {
		storageErr := fmt.Errorf("record turn end: %w", err)
		if executeErr == nil {
			return taskmodel.TaskFailed, contracts.TaskFailureStorage, storageErr
		}
		return taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, errors.Join(executeErr, storageErr)
	}
	return outcome, code, executeErr
}

// settlement 将 loop 结果映射为 Turn 终态；取消优先于普通失败。
func settlement(
	ctx context.Context,
	outcome taskmodel.TaskOutcome,
	code contracts.TaskFailureCode,
	err error,
) (turnmodel.TurnOutcome, contracts.TaskFailureCode, string) {
	if ctx.Err() != nil {
		if code == "" {
			code = contracts.TaskFailureRuntimeCancelled
		}
		return turnmodel.TurnInterrupted, code, ""
	}
	if outcome == taskmodel.TaskCompleted {
		return turnmodel.TurnCompleted, "", ""
	}
	if code == "" {
		code = contracts.TaskFailureRuntimeFailed
	}
	return turnmodel.TurnFailed, code, contracts.TaskFailureMessage(code, err)
}
