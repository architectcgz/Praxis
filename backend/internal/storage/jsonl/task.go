package jsonl

import (
	"cmp"
	"context"
	"math"
	"praxis/internal/fault"
	"praxis/internal/identity"
	taskmodel "praxis/internal/task"
	"slices"
)

type TaskRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r TaskRepository) Get(ctx context.Context, id identity.TaskID) (taskmodel.Task, error) {
	return get[taskmodel.Task](ctx, r.s, "task", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r TaskRepository) Save(ctx context.Context, v taskmodel.Task) error {
	return save(ctx, r.s, v.SessionID.String(), "task", v.ID.String(), "task.saved", v)
}

// FindByRequest 按 Agent 与请求身份查找，未找到时返回 fault.ErrNotFound。
func (r TaskRepository) FindByRequest(ctx context.Context, id identity.AgentID, request identity.RequestID) (taskmodel.Task, error) {
	return getByClaim[taskmodel.Task](ctx, r.s, lookupKey{kind: "task-request", owner: id.String(), value: request.String()})
}

// ListByAgent 返回指定 Agent 的对象副本；非正数 limit 返回全部任务。
func (r TaskRepository) ListByAgent(ctx context.Context, id identity.AgentID, limit int) ([]taskmodel.Task, error) {
	v, e := listIndexed[taskmodel.Task](ctx, r.s, lookupKey{kind: "tasks-agent", owner: id.String()}, nil)
	sorted(v, func(a, b taskmodel.Task) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 {
		v = capped(v, limit)
	}
	return v, e
}

// NextSequence 返回指定 Agent 的下一队列序号，必须在入队事务中调用。
func (r TaskRepository) NextSequence(ctx context.Context, id identity.AgentID) (uint64, error) {
	var sequence uint64
	err := r.s.view(ctx, func(readCtx context.Context) error {
		sequence = r.s.index.maxQueue[id.String()]
		for _, change := range r.s.tx(readCtx).changes {
			if slices.Contains(change.entry.groups, lookupKey{kind: "tasks-agent", owner: id.String()}) {
				sequence = max(sequence, change.entry.sequence)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if sequence == math.MaxUint64 {
		// 拒绝回绕为零，避免后续任务破坏队列的单调顺序。
		return 0, fault.InvalidValue("task.sequence", "task sequence is exhausted")
	}
	return sequence + 1, nil
}

// FindNextPendingByAgent 仅访问该 Agent 的 Pending 索引，返回 FIFO 队首。
func (r TaskRepository) FindNextPendingByAgent(ctx context.Context, id identity.AgentID) (taskmodel.Task, error) {
	var result taskmodel.Task
	err := r.s.view(ctx, func(readCtx context.Context) error {
		var next entityKey
		var sequence uint64
		for _, key := range r.s.keys(readCtx, lookupKey{kind: "pending-agent", owner: id.String()}) {
			entry, _ := r.s.entry(readCtx, key)
			if next.id == "" || entry.sequence < sequence {
				next, sequence = key, entry.sequence
			}
		}
		if next.id == "" {
			return fault.ErrNotFound
		}
		var err error
		result, err = r.Get(readCtx, identity.TaskID(next.id))
		return err
	})
	return result, err
}
