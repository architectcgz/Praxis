// Package turn 负责 loop 迭代的持久化开始与结算，不拥有 Provider 与工具调用的执行。
package turn

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"
	"praxis/internal/repository"
	"praxis/internal/system"

	"praxis/internal/agent_runtime"
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
func (s *Service) RecordStart(ctx context.Context, params agentruntime.TurnParams) (turnmodel.Turn, error) {
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
