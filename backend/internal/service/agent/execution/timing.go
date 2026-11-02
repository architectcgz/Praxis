package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"time"
)

// RecordTiming 是 Recorder 的记录回调；保存失败记录日志并通知界面，不能重放业务操作。
func (s *Service) RecordTiming(run *agentexecution.Run, record timing.Record) {
	execution := run.Snapshot()
	if execution.ID.String() != record.ExecutionID {
		s.logger.Errorf("timing owner mismatch execution=%s", record.ExecutionID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := run.Session.AppendTiming(ctx, record); err != nil {
		s.logger.Errorf("execution timing save failed execution=%s operation=%s status=%s: %v", record.ExecutionID, record.ID, record.Status, err)
		if s.emitter != nil {
			s.emitter.Emit(coreevent.Event{
				Source:  coreevent.SourceForRun(run, ""),
				Payload: coreevent.ErrorOccurred{Message: "Failed to save operation timing: " + err.Error()},
			})
		}
	}
	s.publishTiming(record.View(execution.SessionID.String(), execution.AgentID.String(), execution.Active()), coreevent.SourceForRun(run, ""))
}

func (s *Service) publishTiming(value timing.View, source coreevent.EventSource) {
	if s.emitter != nil {
		s.emitter.Emit(coreevent.Event{
			Source:  source,
			Payload: coreevent.OperationTiming{Timing: value},
		})
	}
}

func (s *Service) publishExecutionTiming(execution agentexecution.ExecutionRecord, run *agentexecution.Run) {
	if value := execution.TimingView(); value != nil {
		s.publishTiming(*value, coreevent.SourceForRun(run, ""))
	}
}
