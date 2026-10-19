// Package timing 独立记录操作耗时，并通过 Hook 接入业务执行边界。
package timing

import (
	"context"
	"errors"
	timingmodel "praxis/internal/core/timing"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
	"time"
)

// Recorder 统一管理单调时钟、持久化和变更通知。存储失败只记日志，不改变业务结果。
type Recorder struct {
	store   repository.Timing
	observe func(timingmodel.Record)
	logf    func(string, ...any)
}

// New 创建记录器；store 不可为空，observer 和 logf 可省略。
func New(store repository.Timing, observer func(timingmodel.Record), logf func(string, ...any)) (*Recorder, error) {
	if store == nil {
		return nil, errors.New("timing store is required")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Recorder{store: store, observe: observer, logf: logf}, nil
}

type spanContextKey struct{}

// Span 是一次操作的计时句柄；并发结束或重复结束只结算一次。
type Span struct {
	mu       sync.Mutex
	recorder *Recorder
	record   timingmodel.Record
	started  time.Time
}

// Start 返回携带父子关联的 context 和计时句柄；同一会话的嵌套操作继承缺省归属。
// nil Recorder 安全退化为空操作，便于未启用计时的测试和独立运行。
func (r *Recorder) Start(ctx context.Context, operation timingmodel.Operation) (context.Context, *Span) {
	if r == nil {
		return ctx, nil
	}
	if ctx == nil {
		r.logf("timing start rejected: context is required")
		return ctx, nil
	}
	parentID := ""
	if parent, ok := ctx.Value(spanContextKey{}).(timingmodel.Record); ok {
		if operation.SessionID == "" {
			operation.SessionID = parent.SessionID
		}
		if parent.SessionID == operation.SessionID {
			if operation.AgentID == "" {
				operation.AgentID = parent.AgentID
			}
			if operation.ExecutionID == "" {
				operation.ExecutionID = parent.ExecutionID
			}
			parentID = parent.ID
		}
	}
	started := time.Now()
	span := &Span{
		recorder: r,
		started:  started,
		record: timingmodel.Record{
			Operation: operation,
			ID:        (identity.SecureIDGenerator{}).New("timing"),
			ParentID:  parentID,
			StartedAt: started.UTC(),
			Status:    timingmodel.Running,
			Revision:  1,
		},
	}
	if err := span.record.Validate(); err != nil {
		r.logf("timing start rejected: %v", err)
		return ctx, nil
	}
	r.publish(span.record)
	return context.WithValue(ctx, spanContextKey{}, span.record), span
}

// FirstResponse 记录首个有效内容到达时间，仅 Provider 使用；重复调用不会重复写入。
func (s *Span) FirstResponse() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Kind != timingmodel.Provider || s.record.Status != timingmodel.Running || s.record.FirstResponseMS != nil {
		return
	}
	elapsed := time.Since(s.started).Milliseconds()
	s.record.FirstResponseMS = &elapsed
	s.record.Revision++
	s.recorder.publish(s.record)
}

// End 按业务结果结束计时；用单调时钟计算耗时，不受系统时间校准影响。
// 取消后的保存使用独立且有上限的 context，避免丢失已完成操作的记录。
func (s *Span) End(status timingmodel.Status) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Status != timingmodel.Running {
		return
	}
	if status != timingmodel.Completed && status != timingmodel.Failed && status != timingmodel.Cancelled {
		status = timingmodel.Failed
	}
	elapsed := time.Since(s.started).Milliseconds()
	s.record.DurationMS = &elapsed
	s.record.FinishedAt = time.Now().UTC()
	s.record.Status = status
	s.record.Revision++
	s.recorder.publish(s.record)
}

// ResultStatus 把执行边界的错误和取消映射成统一状态，取消优先于普通失败。
func ResultStatus(ctx context.Context, err error) timingmodel.Status {
	if ctx != nil && ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return timingmodel.Cancelled
	}
	if err != nil {
		return timingmodel.Failed
	}
	return timingmodel.Completed
}

// ListAgent 返回指定 Agent 最近的记录；数量限制由存储边界执行。
func (r *Recorder) ListAgent(ctx context.Context, agentID string, limit int) ([]timingmodel.Record, error) {
	return r.store.ListAgent(ctx, agentID, limit)
}

func (r *Recorder) publish(record timingmodel.Record) {
	// 计时写入独立于业务事务，不能继承事务 context 或被业务取消提前终止。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.store.Save(ctx, record); err != nil {
		r.logf("timing save failed id=%s status=%s: %v", record.ID, record.Status, err)
	}
	if r.observe != nil {
		r.observe(record)
	}
}
