package coordinator

import (
	"context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
	"time"
)

// StopAgent 中断 Agent 当前正在执行的任务，保留尚未开始的输入。
func (s *Coordinator) StopAgent(ctx context.Context, agentID identity.AgentID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()

	active, applied, err := s.cancelCurrentExecution(ctx, agentID, errExecutionInterrupted)
	if err == nil && applied {
		var notification *coreevent.Event
		s.mu.Lock()
		if active.run != nil {
			at := time.Now().UTC()
			active.run.Agent.ApplyInterrupt(active.executionID, at)
			notification = &coreevent.Event{
				Source:  coreevent.SourceForRun(active.run, ""),
				Payload: coreevent.ExecutionInterruptRequested{AppliedAt: at},
			}
		}
		s.mu.Unlock()
		if notification != nil {
			s.loop.Emitter.Emit(*notification)
		}
	}
	return err
}
