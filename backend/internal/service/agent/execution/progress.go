package execution

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/loop"
	agentservice "praxis/internal/service/agent"
)

// LoadProgress 只向核心循环暴露已提交的预算和输出证据。
func (s *Service) LoadProgress(ctx context.Context, id identity.AgentExecutionID) (loop.Progress, error) {
	execution, err := s.records.Get(ctx, id)
	if err != nil {
		return loop.Progress{}, err
	}
	return loop.Progress{NextSequence: execution.Progress.NextSequence, Iterations: execution.Progress.Iterations, FinalStopReason: execution.Progress.FinalStopReason}, nil
}

// RecordIteration 在真实 ReAct 请求前原子预留一次迭代。
func (s *Service) RecordIteration(ctx context.Context, id identity.AgentExecutionID, sequence uint64) error {
	return s.recordRequest(ctx, id, sequence, true)
}

// RecordSummaryRequest 仅统计摘要请求，不消耗 ReAct 迭代。
func (s *Service) RecordSummaryRequest(ctx context.Context, id identity.AgentExecutionID, sequence uint64) error {
	return s.recordRequest(ctx, id, sequence, false)
}

func (s *Service) recordRequest(ctx context.Context, id identity.AgentExecutionID, sequence uint64, iteration bool) error {
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		task, err := s.records.Get(txCtx, id)
		if err != nil {
			return err
		}
		if !task.Active() || sequence != max(uint64(1), task.Progress.NextSequence) {
			return fault.ErrRequestConflict
		}
		if iteration {
			if task.Progress.Iterations >= agentmodel.MaxIterations {
				return fault.New(agentservice.ExecutionResourceLimit, "model iteration limit exceeded")
			}
			task.Progress.Iterations++
		}
		task.Progress.NextSequence = sequence + 1
		task.Progress.ModelRequests++
		return s.records.Save(txCtx, task)
	})
}

// RecordModelOutput 累计一次模型响应的文本字节；ctx 必须非 nil，取消后仍保存已消耗额度。
// 调用方只记录模型输出；工具回执由消息存储层计费，不在此重复累计。
func (s *Service) RecordModelOutput(ctx context.Context, id identity.AgentExecutionID, outputBytes int) error {
	return s.tx.InTx(context.WithoutCancel(ctx), func(txCtx context.Context) error {
		task, err := s.records.Get(txCtx, id)
		if err != nil {
			return err
		}
		task.Progress.OutputBytes += int64(outputBytes)
		return s.records.Save(txCtx, task)
	})
}
