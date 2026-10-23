package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// NextRequestSequence 返回当前可用序号，执行服务在模型调用前保存请求预留。
func (s *Service) NextRequestSequence(ctx context.Context, id identity.AgentExecutionID) (uint64, error) {
	execution, err := s.records.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	if !execution.Active() {
		return 0, errorcode.ErrRequestConflict
	}
	return max(uint64(1), execution.Progress.NextSequence), nil
}

// update 由同一执行者串行读写执行事实；写入失败直接交给调用方，不重放外部副作用。
func (s *Service) update(ctx context.Context, id identity.AgentExecutionID, change func(*agentexecution.Execution) error) error {
	value, err := s.records.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := change(&value); err != nil {
		return err
	}
	return s.records.Save(ctx, value)
}

// RecordModelRequest 在请求前可靠预留序号并累计次数，不依赖扩展装配。
func (s *Service) RecordModelRequest(ctx context.Context, id identity.AgentExecutionID, sequence uint64, iteration bool) error {
	return s.update(ctx, id, func(value *agentexecution.Execution) error {
		if !value.Active() || sequence != max(uint64(1), value.Progress.NextSequence) {
			return errorcode.ErrRequestConflict
		}
		if iteration {
			value.Progress.Iterations++
		}
		value.Progress.NextSequence = sequence + 1
		value.Progress.ModelRequests++
		return nil
	})
}

// RecordModelOutput 保存已经返回的模型消耗，工具字节始终从回执派生。
func (s *Service) RecordModelOutput(ctx context.Context, id identity.AgentExecutionID, outputBytes int) error {
	if outputBytes == 0 {
		return nil
	}
	return s.update(context.WithoutCancel(ctx), id, func(value *agentexecution.Execution) error {
		value.Progress.ModelOutputBytes += int64(outputBytes)
		return nil
	})
}
