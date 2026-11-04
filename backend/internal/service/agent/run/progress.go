package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/errorcode"
)

// modelProgress 绑定一次执行的模型请求事实提交，不提供 Entry 或上下文访问能力。
type modelProgress struct {
	service *Service
	run     *agentrun.Run
}

func (p *modelProgress) NextRequestSequence(ctx context.Context) (uint64, error) {
	sequence, err := p.service.NextRequestSequence(ctx, p.run)
	if err != nil {
		return 0, storageFailure(err)
	}
	return sequence, nil
}

func (p *modelProgress) RecordModelRequest(ctx context.Context, sequence uint64, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	if err := p.service.RecordModelRequest(ctx, p.run, sequence, request, measured); err != nil {
		return storageFailure(err)
	}
	return nil
}

func (p *modelProgress) RecordModelResponse(ctx context.Context, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	return p.service.RecordModelResponse(ctx, p.run, request, measured)
}

func (s *Service) NextRequestSequence(ctx context.Context, run *agentrun.Run) (uint64, error) {
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
func (s *Service) update(ctx context.Context, run *agentrun.Run, change func(*agentrun.RunRecord) error) error {
	var writeErr error
	err := run.Update(func(record *agentrun.RunRecord) error {
		if err := change(record); err != nil {
			return err
		}
		writeErr = s.records.SaveRun(ctx, *record)
		return nil
	})
	if err != nil {
		return err
	}
	return writeErr
}

func (s *Service) RecordModelRequest(ctx context.Context, run *agentrun.Run, sequence uint64, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	var owner agentrun.RunRecord
	if err := run.Update(func(record *agentrun.RunRecord) error {
		if !record.Active() || sequence != max(uint64(1), record.Progress.NextSequence) {
			return errorcode.ErrRequestConflict
		}
		if request.Kind == agentrun.ModelRequestIteration {
			record.Progress.Iterations++
		}
		record.Progress.NextSequence = sequence + 1
		record.Progress.ModelRequests++
		owner = *record
		return nil
	}); err != nil {
		return err
	}
	if request.Kind == agentrun.ModelRequestIteration {
		s.emitter.Emit(coreevent.Event{
			Source: coreevent.SourceForRun(run, request.TurnID),
			Payload: coreevent.ModelRequestStarted{
				Request: request,
				Timing:  measured,
			},
		})
	}
	return s.records.SaveModelRequest(ctx, owner, request)
}
