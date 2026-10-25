package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
)

func (s *Service) NextRequestSequence(ctx context.Context, runtime *agentexecution.Runtime) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	record := runtime.Snapshot()
	if !record.Active() {
		return 0, errorcode.ErrRequestConflict
	}
	return max(uint64(1), record.Progress.NextSequence), nil
}

// update 不回读 Repository；Main 先由共享内存仓储接纳快照，子执行只更新本次 Runtime。
func (s *Service) update(ctx context.Context, runtime *agentexecution.Runtime, change func(*agentexecution.Execution) error) error {
	return runtime.Update(func(record *agentexecution.Execution) error {
		if err := change(record); err != nil {
			return err
		}
		if runtime.Main() {
			return s.records.Save(ctx, *record)
		}
		return ctx.Err()
	})
}

func (s *Service) RecordModelRequest(ctx context.Context, runtime *agentexecution.Runtime, sequence uint64, iteration bool) error {
	return s.update(ctx, runtime, func(record *agentexecution.Execution) error {
		if !record.Active() || sequence != max(uint64(1), record.Progress.NextSequence) {
			return errorcode.ErrRequestConflict
		}
		if iteration {
			record.Progress.Iterations++
		}
		record.Progress.NextSequence = sequence + 1
		record.Progress.ModelRequests++
		return nil
	})
}

func (s *Service) RecordModelOutput(ctx context.Context, runtime *agentexecution.Runtime, outputBytes int) error {
	if outputBytes == 0 {
		return nil
	}
	return s.update(context.WithoutCancel(ctx), runtime, func(record *agentexecution.Execution) error {
		record.Progress.ModelOutputBytes += int64(outputBytes)
		return nil
	})
}
