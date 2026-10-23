package storage

import (
	"cmp"
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// timingKey 将一次操作尝试绑定到所属 Execution，开始和结束共用同一个键。
func timingKey(executionID, operationID string) string { return entryKey(executionID, operationID) }

// AppendTiming 只追加开始或结束事实，不保存整个 Execution；相同快照幂等。
func (r AgentExecutionRepository) AppendTiming(ctx context.Context, value timing.Record) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.Status == timing.Interrupted {
		return errors.New("interrupted timing is a replay projection")
	}
	return r.s.view(ctx, func() error {
		key := timingKey(value.ExecutionID, value.ID)
		old, err := getLocked[timing.Record](r.s, "agent_execution_timing", key)
		if err == nil {
			if same(old, value) {
				return nil
			}
			if old.Status != timing.Running {
				return errorcode.ErrRequestConflict
			}
		} else if !errors.Is(err, errorcode.ErrNotFound) {
			return err
		}
		execution, err := getLocked[agentexecution.Execution](r.s, "agent_execution", value.ExecutionID)
		if err != nil {
			return err
		}
		kind := "agent_execution.timing_finished"
		if value.Status == timing.Running {
			kind = "agent_execution.timing_started"
		}
		// 开始写入失败时，仍允许完整结束快照成为唯一事实。
		return r.s.appendLocked(ctx, execution.SessionID.String(), "agent_execution_timing", key, kind, value)
	})
}

func (r AgentExecutionRepository) ListTimings(ctx context.Context, id identity.AgentExecutionID) ([]timing.Record, error) {
	var result []timing.Record
	err := r.s.view(ctx, func() error {
		if _, err := getLocked[agentexecution.Execution](r.s, "agent_execution", id.String()); err != nil {
			return err
		}
		var err error
		result, err = listIndexedLocked[timing.Record](r.s, lookupKey{kind: "timings-execution", owner: id.String()}, nil)
		return err
	})
	return sortTimings(result), err
}

func (r AgentExecutionRepository) ListAgentTimings(ctx context.Context, id identity.AgentID, limit int) ([]timing.Record, error) {
	values, err := listIndexed[timing.Record](ctx, r.s, lookupKey{kind: "timings-agent", owner: id.String()}, nil)
	return capped(sortTimings(values), limit), err
}

func sortTimings(values []timing.Record) []timing.Record {
	return sorted(values, func(a, b timing.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})
}

// projectInterruptedTimings 只在启动 Replay 全部完成后收敛旧进程的未结束操作。
// 保留开始事实，不追加伪造结束事件，也不改变工具恢复决策。
func (s *Store) projectInterruptedTimings() {
	for _, key := range s.keys(lookupKey{kind: "running-timings"}) {
		entry, _ := s.entry(key)
		entry.interrupted = true
		s.state.entries[key] = entry
	}
}
