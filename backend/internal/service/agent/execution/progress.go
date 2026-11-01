package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
)

// modelProgress 仅绑定一次执行的模型计数，不提供 Entry 或上下文访问能力。
type modelProgress struct {
	service *Service
	run     *agentexecution.Run
}

func (p *modelProgress) NextRequestSequence(ctx context.Context) (uint64, error) {
	sequence, err := p.service.NextRequestSequence(ctx, p.run)
	if err != nil {
		return 0, storageFailure(err)
	}
	return sequence, nil
}

func (p *modelProgress) RecordModelRequest(ctx context.Context, sequence uint64, iteration bool) error {
	if err := p.service.RecordModelRequest(ctx, p.run, sequence, iteration); err != nil {
		return storageFailure(err)
	}
	return nil
}

func (p *modelProgress) RecordModelResponse(ctx context.Context, request agentexecution.ModelRequestRecord) error {
	return p.service.RecordModelResponse(ctx, p.run, request)
}

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

// update 不回读历史；主、子执行同步保存快照，写入失败不阻止 Run 接纳新进度。
func (s *Service) update(ctx context.Context, run *agentexecution.Run, change func(*agentexecution.ExecutionRecord) error) error {
	var writeErr error
	err := run.Update(func(record *agentexecution.ExecutionRecord) error {
		if err := change(record); err != nil {
			return err
		}
		writeErr = s.records.SaveExecution(ctx, *record)
		return nil
	})
	if err != nil {
		return err
	}
	return writeErr
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
