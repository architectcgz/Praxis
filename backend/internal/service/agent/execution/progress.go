package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
)

func (s *Service) NextRequestSequence(ctx context.Context, run *agentexecution.Run) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	record := run.Snapshot()
	if !record.Active() {
		return 0, errorcode.ErrRequestConflict
	}
	return max(uint64(1), record.Progress.NextSequence), nil
}

// update 不回读 Repository；Main 先由共享内存仓储接纳快照，子执行只更新本次 Run。
func (s *Service) update(ctx context.Context, run *agentexecution.Run, change func(*agentexecution.ExecutionRecord) error) error {
	return run.Update(func(record *agentexecution.ExecutionRecord) error {
		if err := change(record); err != nil {
			return err
		}
		if run.Main() {
			return s.records.Save(ctx, *record)
		}
		return ctx.Err()
	})
}

func (s *Service) RecordModelRequest(ctx context.Context, run *agentexecution.Run, sequence uint64, iteration bool) error {
	return s.update(ctx, run, func(record *agentexecution.ExecutionRecord) error {
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

func (s *Service) RecordModelOutput(ctx context.Context, run *agentexecution.Run, outputBytes int) error {
	if outputBytes == 0 {
		return nil
	}
	return s.update(context.WithoutCancel(ctx), run, func(record *agentexecution.ExecutionRecord) error {
		record.Progress.ModelOutputBytes += int64(outputBytes)
		return nil
	})
}
