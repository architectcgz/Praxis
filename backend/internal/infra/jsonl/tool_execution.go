package jsonl

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolexecution "praxis/internal/core/tool_execution"
	"praxis/internal/repository"
)

// ToolExecutionRepository 复用 Store 的事务与消息流保存执行记录，不持有独立文件或锁。
type ToolExecutionRepository struct{ s *Store }

// ReserveTool 在同一事务内检查固定工具额度并幂等预留，不更新 Task 的重复计数。
func (r ToolExecutionRepository) ReserveTool(ctx context.Context, key toolexecution.CallKey) (bool, error) {
	inserted := false
	err := r.s.InTx(ctx, func(txCtx context.Context) error {
		state, err := r.s.rebuildToolState(txCtx, key, nil)
		if err != nil || state.Reserved {
			return err
		}
		usage, err := r.ToolUsage(txCtx, key.SessionID, key.TaskID)
		if err != nil {
			return err
		}
		if usage.Calls >= taskmodel.MaxToolCalls {
			return contracts.New(contracts.TaskResourceLimit, "tool call limit exceeded")
		}
		if err := r.appendToolRecord(txCtx, key, toolexecution.RecordReserved, nil, nil); err != nil {
			return err
		}
		inserted = true
		return nil
	})
	return inserted && err == nil, err
}

// PrepareTool 冻结执行参数；重复准备只能提交相同内容。
func (r ToolExecutionRepository) PrepareTool(ctx context.Context, key toolexecution.CallKey, prepared toolexecution.Preparation) error {
	return r.s.InTx(ctx, func(txCtx context.Context) error {
		state, err := r.s.rebuildToolState(txCtx, key, nil)
		if err != nil {
			return err
		}
		if state.Prepared != nil {
			if same(*state.Prepared, prepared) {
				return nil
			}
			return contracts.ErrRequestConflict
		}
		return r.appendToolRecord(txCtx, key, toolexecution.RecordPrepared, &prepared, nil)
	})
}

// RequestToolApproval 以新事件撤销尚未开始的旧授权，不改写历史审批。
func (r ToolExecutionRepository) RequestToolApproval(ctx context.Context, key toolexecution.CallKey) error {
	return r.s.InTx(ctx, func(txCtx context.Context) error {
		state, err := r.s.rebuildToolState(txCtx, key, nil)
		if err != nil || state.Status == toolexecution.StatusAwaitingApproval {
			return err
		}
		return r.appendToolRecord(txCtx, key, toolexecution.RecordApprovalRequested, nil, nil)
	})
}

// RecordToolApproval 绑定冻结参数的摘要；审批时间与来源相同的重试不重复追加。
func (r ToolExecutionRepository) RecordToolApproval(ctx context.Context, key toolexecution.CallKey, approval contracts.ApprovalRecord) error {
	return r.s.InTx(ctx, func(txCtx context.Context) error {
		state, err := r.s.rebuildToolState(txCtx, key, nil)
		if err != nil {
			return err
		}
		if state.Approval != nil {
			if same(*state.Approval, approval) {
				return nil
			}
			return contracts.ErrRequestConflict
		}
		return r.appendToolRecord(txCtx, key, toolexecution.RecordApproved, nil, &approval)
	})
}

// TryStartTool 仅在独立事务可靠提交后返回资格，不能嵌入可能回滚的外层事务。
func (r ToolExecutionRepository) TryStartTool(ctx context.Context, key toolexecution.CallKey, authorize func(context.Context, toolexecution.State) error) (bool, error) {
	if authorize == nil || r.s.tx(ctx) != nil {
		return false, errors.New("tool start requires an authorization check and its own commit boundary")
	}
	started := false
	err := r.s.InTx(ctx, func(txCtx context.Context) error {
		state, err := r.s.rebuildToolState(txCtx, key, nil)
		if err != nil {
			return err
		}
		if !state.StartedAt.IsZero() || state.ResultMessageID != "" {
			return nil
		}
		if state.Status != toolexecution.StatusApproved {
			return errors.New("tool call is not approved")
		}
		before := len(r.s.tx(txCtx).events)
		if err := authorize(txCtx, state); err != nil {
			return err
		}
		if len(r.s.tx(txCtx).events) != before {
			return errors.New("tool authorization must not modify the transaction")
		}
		if err := r.appendToolRecord(txCtx, key, toolexecution.RecordStarted, nil, nil); err != nil {
			return err
		}
		started = true
		return nil
	})
	return started && err == nil, err
}

// AppendToolResult 将正文与终态写入现有消息记录，计费只统计已提交正文。
func (r ToolExecutionRepository) AppendToolResult(ctx context.Context, key toolexecution.CallKey, data sessionmodel.MessageData, outcome toolexecution.Outcome) (bool, error) {
	inserted := false
	err := r.s.InTx(ctx, func(txCtx context.Context) error {
		state, err := r.s.rebuildToolState(txCtx, key, nil)
		if err != nil {
			return err
		}
		if data.Role != sessionmodel.RoleTool || data.TaskID != key.TaskID.String() || len(data.Blocks) != 1 || data.Blocks[0].CallID != key.CallID {
			return errors.New("tool result ownership does not match the call")
		}
		if state.ResultMessageID != "" && state.ResultMessageID != data.ID {
			return contracts.ErrRequestConflict
		}
		owner, err := (MessageStreams{r.s}).owner(txCtx, key.SessionID, key.AgentID)
		if err != nil {
			return err
		}
		_, inserted, err = r.s.appendMessage(txCtx, owner, data, &outcome)
		return err
	})
	return inserted && err == nil, err
}

// GetToolState 从消息与事件位置重建状态，不缓存领域正文。
func (r ToolExecutionRepository) GetToolState(ctx context.Context, key toolexecution.CallKey) (toolexecution.State, error) {
	var state toolexecution.State
	err := r.s.view(ctx, func(readCtx context.Context) error {
		var err error
		state, err = r.s.rebuildToolState(readCtx, key, nil)
		return err
	})
	return state, err
}

// ListToolStates 按声明序号与调用顺序返回 Session 的工具状态。
func (r ToolExecutionRepository) ListToolStates(ctx context.Context, sessionID contracts.SessionID) ([]toolexecution.State, error) {
	var result []toolexecution.State
	err := r.s.view(ctx, func(readCtx context.Context) error {
		var err error
		result, err = r.s.rebuildToolStates(readCtx, sessionID, "")
		return err
	})
	return result, err
}

// ToolUsage 汇总 Task 已提交的预留次数和结果字节，不修改业务存储。
func (r ToolExecutionRepository) ToolUsage(ctx context.Context, sessionID contracts.SessionID, taskID contracts.TaskID) (repository.ToolUsage, error) {
	var usage repository.ToolUsage
	if taskID == "" {
		return usage, errors.New("tool usage requires a task identity")
	}
	err := r.s.view(ctx, func(readCtx context.Context) error {
		states, err := r.s.rebuildToolStates(readCtx, sessionID, taskID)
		if err != nil {
			return err
		}
		for _, state := range states {
			if state.Reserved {
				usage.Calls++
			}
			usage.OutputBytes += state.OutputBytes
		}
		return nil
	})
	return usage, err
}

func (r ToolExecutionRepository) appendToolRecord(ctx context.Context, key toolexecution.CallKey, kind toolexecution.RecordType, prepared *toolexecution.Preparation, approval *contracts.ApprovalRecord) error {
	sequence := r.s.sequences[key.SessionID.String()] + 1
	if tx := r.s.tx(ctx); len(tx.events) > 0 {
		sequence = tx.nextSequence + 1
	}
	return save(ctx, r.s, key.SessionID.String(), "tool_execution", fmt.Sprintf("event:%d", sequence), string(kind), toolExecutionRecord{
		Call:     key,
		Prepared: prepared,
		Approval: approval,
	})
}

var _ repository.ToolExecutionRepository = ToolExecutionRepository{}
