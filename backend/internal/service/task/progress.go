package task

import (
	"context"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// LoadTask 读取已提交的 Task供 Loop 恢复预算；不存在时返回 ErrNotFound。
func (s *Service) LoadTask(ctx context.Context, id contracts.TaskID) (taskmodel.Task, error) {
	return s.tasks.Get(ctx, id)
}

// RecordIteration 在真实 ReAct 请求前原子预留一次迭代。
func (s *Service) RecordIteration(ctx context.Context, id contracts.TaskID, sequence uint64) error {
	return s.recordRequest(ctx, id, sequence, true)
}

// RecordSummaryRequest 仅统计摘要请求，不消耗 ReAct 迭代。
func (s *Service) RecordSummaryRequest(ctx context.Context, id contracts.TaskID, sequence uint64) error {
	return s.recordRequest(ctx, id, sequence, false)
}

func (s *Service) recordRequest(ctx context.Context, id contracts.TaskID, sequence uint64, iteration bool) error {
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		task, err := s.tasks.Get(txCtx, id)
		if err != nil {
			return err
		}
		if !task.Active() || sequence != max(uint64(1), task.Progress.NextSequence) {
			return contracts.ErrRequestConflict
		}
		if iteration {
			if task.Progress.Iterations >= taskmodel.MaxIterations {
				return contracts.New(contracts.TaskResourceLimit, "model iteration limit exceeded")
			}
			task.Progress.Iterations++
		}
		task.Progress.NextSequence = sequence + 1
		task.Progress.ModelRequests++
		return s.tasks.Save(txCtx, task)
	})
}

// RecordModelOutput 累计一次模型响应的文本字节；ctx 必须非 nil，取消后仍保存已消耗额度。
// 调用方只记录模型输出；工具回执由消息存储层计费，不在此重复累计。
func (s *Service) RecordModelOutput(ctx context.Context, id contracts.TaskID, outputBytes int) error {
	return s.tx.InTx(context.WithoutCancel(ctx), func(txCtx context.Context) error {
		task, err := s.tasks.Get(txCtx, id)
		if err != nil {
			return err
		}
		task.Progress.OutputBytes += int64(outputBytes)
		return s.tasks.Save(txCtx, task)
	})
}
