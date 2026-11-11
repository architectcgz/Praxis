package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// runRecorder 绑定一次执行的轮次与模型请求记录，不提供 Entry 或上下文访问能力。
type runRecorder struct {
	service *Service
	run     *agentrun.Run
}

func (p *runRecorder) BeginTurn(ctx context.Context) (identity.TurnID, error) {
	return p.service.BeginRequestTurn(ctx, p.run)
}

func (s *Service) BeginRequestTurn(ctx context.Context, run *agentrun.Run) (identity.TurnID, error) {
	record := run.Snapshot()
	if record.CurrentTurn.RequestID == "" && record.CurrentTurn.EndedAt.IsZero() {
		return record.CurrentTurn.ID, nil
	}
	if err := s.update(ctx, run, func(record *agentrun.RunRecord) error {
		record.BeginTurn(s.clock.Now())
		return nil
	}); err != nil {
		return "", storageFailure(err)
	}
	return run.Snapshot().CurrentTurn.ID, nil
}

func (p *runRecorder) EndTurn(ctx context.Context) error {
	record := p.run.Snapshot()
	// 尚未发出请求的准入轮次由下一次实际模型请求复用。
	if record.CurrentTurn.RequestID == "" || !record.CurrentTurn.EndedAt.IsZero() {
		return nil
	}
	if err := p.service.update(ctx, p.run, func(record *agentrun.RunRecord) error {
		record.EndTurn(p.service.clock.Now())
		return nil
	}); err != nil {
		return storageFailure(err)
	}
	return nil
}

func (p *runRecorder) RecordModelRequest(ctx context.Context, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	if err := p.service.RecordModelRequest(ctx, p.run, request, measured); err != nil {
		return storageFailure(err)
	}
	return nil
}

func (p *runRecorder) RecordModelResponse(ctx context.Context, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	return p.service.RecordModelResponse(ctx, p.run, request, measured)
}

// update 不回读历史；主、子执行同步保存快照，写入失败不阻止 Run 接纳新进度。
func (s *Service) update(ctx context.Context, run *agentrun.Run, change func(*agentrun.RunRecord) error) error {
	if err := run.Update(change); err != nil {
		return err
	}
	_, err := s.records.SaveRun(ctx, run.Snapshot())
	return err
}

func (s *Service) RecordModelRequest(ctx context.Context, run *agentrun.Run, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	var owner agentrun.RunRecord
	if err := run.Update(func(record *agentrun.RunRecord) error {
		if !record.Active() {
			return errorcode.ErrRequestConflict
		}
		record.CurrentTurn.RequestID = request.ID
		if request.Kind == agentrun.ModelRequestIteration {
			record.Progress.Iterations++
		}
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
