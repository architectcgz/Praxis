package execution

import (
	"context"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// NextRequestSequence 返回当前可用序号；同步请求 Hook 在模型调用前校验并原子推进。
// 此操作仅协调正常执行中的请求身份，不执行状态恢复或读取上下文。
func (s *Service) NextRequestSequence(ctx context.Context, id identity.AgentExecutionID) (uint64, error) {
	execution, err := s.records.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	if !execution.Active() {
		return 0, fault.ErrRequestConflict
	}
	return max(uint64(1), execution.Progress.NextSequence), nil
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
			task.Progress.Iterations++
		}
		task.Progress.NextSequence = sequence + 1
		task.Progress.ModelRequests++
		return s.records.Save(txCtx, task)
	})
}

// recordModelOutput 累计模型实际返回的文本字节，取消后仍保存已消耗额度。
// 工具回执由 ToolExecution Service 在回执事务中累计。
func (s *Service) recordModelOutput(ctx context.Context, id identity.AgentExecutionID, outputBytes int) error {
	if outputBytes == 0 {
		return nil
	}
	return s.tx.InTx(context.WithoutCancel(ctx), func(txCtx context.Context) error {
		task, err := s.records.Get(txCtx, id)
		if err != nil {
			return err
		}
		task.Progress.OutputBytes += int64(outputBytes)
		return s.records.Save(txCtx, task)
	})
}
