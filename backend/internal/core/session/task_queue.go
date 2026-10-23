package session

import (
	"fmt"
	"math"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
	"sync"
)

// TaskQueue 属于单个 Session，待办只驻留内存；零值可用。
// 同时最多领取一项，准备失败 Retry，保存 Execution 后 Complete。
type TaskQueue struct {
	mu       sync.Mutex
	pending  []Task
	claimed  *Task
	sequence uint64
}

// Enqueue 只分配 FIFO 顺序，不创建执行身份。
func (q *TaskQueue) Enqueue(value Task) (Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sequence == math.MaxUint64 {
		return Task{}, fmt.Errorf("task.sequence: task sequence is exhausted: %w", errorcode.ErrInvalidValue)
	}
	q.sequence++
	value.Sequence = q.sequence
	q.pending = append(q.pending, value)
	return value, nil
}

// Next 领取队首，不绑定 Agent；取消与领取在同一把锁下仲裁。
func (q *TaskQueue) Next() (Task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 || q.claimed != nil {
		return Task{}, false
	}
	value := q.pending[0]
	q.pending = slices.Delete(q.pending, 0, 1)
	if len(q.pending) == 0 {
		q.pending = nil
	}
	q.claimed = &value
	return value, true
}

// Retry 将未准入输入放回原队首；Clear 后的延迟清理不能重建已释放的队列内容。
func (q *TaskQueue) Retry(value Task) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.claimed != nil && q.claimed.ID == value.ID {
		q.pending = slices.Insert(q.pending, 0, value)
		q.claimed = nil
	}
}

// Complete 只在 Execution 保存成功后释放领取，正文随之退出内存队列。
func (q *TaskQueue) Complete(value Task) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.claimed != nil && q.claimed.ID == value.ID {
		q.claimed = nil
	}
}

// Cancel 只移除未领取输入，不取消当前 Execution，也不保留取消墓碑。
func (q *TaskQueue) Cancel(id identity.TaskID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.claimed != nil && q.claimed.ID == id {
		return errorcode.ErrRequestConflict
	}
	index := slices.IndexFunc(q.pending, func(value Task) bool { return value.ID == id })
	if index < 0 {
		return errorcode.ErrNotFound
	}
	q.pending = slices.Delete(q.pending, index, index+1)
	if len(q.pending) == 0 {
		q.pending = nil
	}
	return nil
}

// Clear 释放正文、底层切片和领取引用；实例的生命周期由所属 Session 管理。
func (q *TaskQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = nil
	q.claimed = nil
	q.sequence = 0
}

// List 返回待办快照，包含尚未保存 Execution 的已领取队首。
func (q *TaskQueue) List(limit int) []Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	values := slices.Clone(q.pending)
	if q.claimed != nil {
		values = slices.Insert(values, 0, *q.claimed)
	}
	if limit > 0 {
		values = values[:min(len(values), limit)]
	}
	return values
}
