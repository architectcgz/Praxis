package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/identity"
)

func timingKey(executionID, operationID string) string {
	return fmt.Sprintf("%d:%s%s", len(executionID), executionID, operationID)
}

func (r AgentExecutionRepository) AppendTiming(ctx context.Context, value timing.Record) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.Status == timing.Interrupted {
		return errors.New("interrupted timing is a read projection")
	}
	return withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		execution, err := getRecord[agentexecution.Execution](records, "agent_execution", value.ExecutionID)
		if err != nil {
			return err
		}
		kind := "agent_execution.timing_finished"
		if value.Status == timing.Running {
			kind = "agent_execution.timing_started"
		}
		return appendRecord(state, records, execution.SessionID.String(), "agent_execution_timing", timingKey(value.ExecutionID, value.ID), kind, value)
	})
}

func (r AgentExecutionRepository) ListTimings(ctx context.Context, id identity.AgentExecutionID) ([]timing.Record, error) {
	var result []timing.Record
	err := withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		if _, err := getRecord[agentexecution.Execution](records, "agent_execution", id.String()); err != nil {
			return err
		}
		var err error
		result, err = listRecords(records, "agent_execution_timing", func(value timing.Record) bool { return value.ExecutionID == id.String() })
		return err
	})
	return sortTimings(result), err
}

func (r AgentExecutionRepository) ListAgentTimings(ctx context.Context, id identity.AgentID, limit int) ([]timing.Record, error) {
	var result []timing.Record
	err := withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		executions, err := listRecords(records, "agent_execution", func(value agentexecution.Execution) bool { return value.AgentID == id })
		if err != nil {
			return err
		}
		owners := make(map[string]bool, len(executions))
		for _, execution := range executions {
			owners[execution.ID.String()] = true
		}
		result, err = listRecords(records, "agent_execution_timing", func(value timing.Record) bool { return owners[value.ExecutionID] })
		return err
	})
	return capped(sortTimings(result), limit), err
}

func sortTimings(values []timing.Record) []timing.Record {
	return sorted(values, func(a, b timing.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})
}
