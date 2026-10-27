package execution

import (
	"cmp"
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
	"slices"
	"time"
)

// RecordTiming 是 Recorder 的记录回调；观测失败只记日志，不能重放业务操作。
func (s *Service) RecordTiming(run *agentexecution.Run, record timing.Record) {
	execution := run.Snapshot()
	if execution.ID.String() != record.ExecutionID {
		s.logger.Errorf("timing owner mismatch execution=%s", record.ExecutionID)
		return
	}
	if run.Main() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.records.AppendTiming(ctx, record); err != nil {
			s.logger.Errorf("execution timing save failed execution=%s operation=%s status=%s: %v", record.ExecutionID, record.ID, record.Status, err)
		}
	} else {
		run.RecordTiming(record)
	}
	s.publishTiming(operationTiming(execution, record))
}

func RunTimings(run *agentexecution.Run) []timing.View {
	execution := run.Snapshot()
	result := make([]timing.View, 0)
	if value := executionTiming(execution); value != nil {
		result = append(result, *value)
	}
	for _, record := range run.Timings() {
		result = append(result, operationTiming(execution, record))
	}
	return result
}

// ListAgentTimings 从 Execution 仓储聚合操作记录，并派生执行本身的墙上时钟耗时。
func (s *Service) ListAgentTimings(ctx context.Context, agentID string, limit int) ([]timing.View, error) {
	records, err := s.records.ListAgentTimings(ctx, identity.AgentID(agentID), limit)
	if err != nil {
		return nil, err
	}
	executions, err := s.records.ListByAgent(ctx, identity.AgentID(agentID), limit)
	if err != nil {
		return nil, err
	}
	owners := make(map[string]agentexecution.ExecutionRecord, len(executions))
	result := make([]timing.View, 0, len(records)+len(executions))
	for _, execution := range executions {
		owners[execution.ID.String()] = execution
		if value := executionTiming(execution); value != nil {
			result = append(result, *value)
		}
	}
	for _, record := range records {
		execution, ok := owners[record.ExecutionID]
		if !ok {
			execution, err = s.records.Get(ctx, identity.AgentExecutionID(record.ExecutionID))
			if err != nil {
				return nil, err
			}
			owners[record.ExecutionID] = execution
		}
		result = append(result, operationTiming(execution, record))
	}
	slices.SortFunc(result, func(a, b timing.View) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func operationTiming(execution agentexecution.ExecutionRecord, record timing.Record) timing.View {
	revision := 1
	if record.Status != timing.Running {
		revision = 2
	}
	return timing.View{
		ID:              record.ID,
		ParentID:        record.ParentID,
		SessionID:       execution.SessionID.String(),
		AgentID:         execution.AgentID.String(),
		ExecutionID:     record.ExecutionID,
		Kind:            string(record.Kind),
		Name:            record.Name,
		ReferenceID:     record.ReferenceID,
		StartedAt:       record.StartedAt,
		FinishedAt:      record.FinishedAt,
		DurationMS:      record.DurationMS,
		FirstResponseMS: record.FirstResponseMS,
		Status:          string(record.Status),
		Revision:        revision,
	}
}

func executionTiming(execution agentexecution.ExecutionRecord) *timing.View {
	if execution.StartedAt.IsZero() {
		return nil
	}
	value := &timing.View{
		ID:          "execution:" + execution.ID.String(),
		SessionID:   execution.SessionID.String(),
		AgentID:     execution.AgentID.String(),
		ExecutionID: execution.ID.String(),
		Kind:        "agent",
		Name:        execution.Config.AgentDefinitionID.String(),
		StartedAt:   execution.StartedAt,
		Status:      string(timing.Running),
		Revision:    1,
	}
	if !execution.EndedAt.IsZero() {
		elapsed := execution.EndedAt.Sub(execution.StartedAt).Milliseconds()
		value.DurationMS, value.FinishedAt, value.Revision = &elapsed, execution.EndedAt, 2
		value.Status = string(timing.Completed)
		switch execution.Outcome {
		case agentexecution.ExecutionFailed:
			value.Status = string(timing.Failed)
		case agentexecution.ExecutionInterrupted, agentexecution.ExecutionPaused:
			value.Status = string(timing.Cancelled)
		}
	}
	return value
}

func (s *Service) publishTiming(value timing.View) {
	if s.emitter != nil {
		s.emitter.Emit(coreevent.Event{
			Scope: coreevent.Scope{
				SessionID:   identity.SessionID(value.SessionID),
				AgentID:     identity.AgentID(value.AgentID),
				ExecutionID: identity.AgentExecutionID(value.ExecutionID),
			},
			Payload: coreevent.OperationTiming{Timing: value},
		})
	}
}

func (s *Service) publishExecutionTiming(execution agentexecution.ExecutionRecord) {
	if value := executionTiming(execution); value != nil {
		s.publishTiming(*value)
	}
}
