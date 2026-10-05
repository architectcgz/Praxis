// Package timing 独立记录操作耗时，不依赖 Agent、Provider、Tool 或消息模型。
package timing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"praxis/internal/system"
)

type Kind string

type Status string

const (
	Agent    Kind = "agent"
	Provider Kind = "provider"
	Tool     Kind = "tool"

	Running     Status = "running"
	Completed   Status = "completed"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

// Operation 只携带关联标识；禁止写入 Prompt、工具参数、响应正文或凭据。
type Operation struct {
	SessionID   string `json:"sessionId"`
	AgentID     string `json:"agentId"`
	TurnID      string `json:"turnId"`
	Kind        Kind   `json:"kind"`
	Name        string `json:"name"`
	ReferenceID string `json:"referenceId,omitempty"`
}

// Record 是独立的计时事实。DurationMS 为 nil 表示尚未结束或异常退出后无法确定耗时。
type Record struct {
	Operation
	ID              string    `json:"id"`
	ParentID        string    `json:"parentId,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
	Status          Status    `json:"status"`
	Revision        int       `json:"revision"`
}

// Validate 只读校验存储恢复和写入边界；不修正非法字段，也不推算未知耗时。
func (r Record) Validate() error {
	for _, value := range []string{r.ID, r.SessionID, r.AgentID, r.TurnID, r.Name} {
		if value == "" || value != strings.TrimSpace(value) {
			return errors.New("timing identity and name must be canonical and nonempty")
		}
	}
	if r.Kind != Agent && r.Kind != Provider && r.Kind != Tool {
		return errors.New("unknown timing kind")
	}
	if r.StartedAt.IsZero() || r.Revision < 1 || r.ParentID == r.ID {
		return errors.New("invalid timing identity, timestamp or revision")
	}
	if r.DurationMS != nil && *r.DurationMS < 0 || r.FirstResponseMS != nil && *r.FirstResponseMS < 0 {
		return errors.New("negative operation duration")
	}
	if r.FirstResponseMS != nil && (r.Kind != Provider || r.DurationMS != nil && *r.FirstResponseMS > *r.DurationMS) {
		return errors.New("invalid first response duration")
	}
	switch r.Status {
	case Running:
		if r.DurationMS != nil || !r.FinishedAt.IsZero() {
			return errors.New("running timing has terminal fields")
		}
	case Completed, Failed, Cancelled:
		if r.DurationMS == nil || r.FinishedAt.IsZero() {
			return errors.New("finished timing requires duration and timestamp")
		}
	case Interrupted:
		if r.DurationMS != nil || !r.FinishedAt.IsZero() {
			return errors.New("interrupted timing cannot claim an exact end")
		}
	default:
		return errors.New("unknown timing status")
	}
	return nil
}

// Store 持久化计时记录；Save 必须拒绝旧版本覆盖新版本及终态回退。
type Store interface {
	Save(context.Context, Record) error
	ListAgent(context.Context, string, int) ([]Record, error)
}

// Recorder 统一管理单调时钟、持久化和变更通知。存储失败只记日志，不改变业务结果。
type Recorder struct {
	store   Store
	observe func(Record)
	logf    func(string, ...any)
}

// New 创建记录器；store 不可为空，observer 和 logf 可省略。
func New(store Store, observer func(Record), logf func(string, ...any)) (*Recorder, error) {
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
	record   Record
	started  time.Time
}

// Start 返回携带父子关联的 context 和计时句柄；同一会话的嵌套操作继承缺省归属。
// nil Recorder 安全退化为空操作，便于未启用计时的测试和独立运行。
func (r *Recorder) Start(ctx context.Context, operation Operation) (context.Context, *Span) {
	if r == nil {
		return ctx, nil
	}
	if ctx == nil {
		r.logf("timing start rejected: context is required")
		return ctx, nil
	}
	parentID := ""
	if parent, ok := ctx.Value(spanContextKey{}).(Record); ok {
		if operation.SessionID == "" {
			operation.SessionID = parent.SessionID
		}
		if parent.SessionID == operation.SessionID {
			if operation.AgentID == "" {
				operation.AgentID = parent.AgentID
			}
			if operation.TurnID == "" {
				operation.TurnID = parent.TurnID
			}
			parentID = parent.ID
		}
	}
	started := time.Now()
	span := &Span{
		recorder: r,
		started:  started,
		record: Record{
			Operation: operation,
			ID:        (system.SecureIDGenerator{}).New("timing"),
			ParentID:  parentID,
			StartedAt: started.UTC(),
			Status:    Running,
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
	if s.record.Kind != Provider || s.record.Status != Running || s.record.FirstResponseMS != nil {
		return
	}
	elapsed := time.Since(s.started).Milliseconds()
	s.record.FirstResponseMS = &elapsed
	s.record.Revision++
	s.recorder.publish(s.record)
}

// End 按业务结果结束计时；用单调时钟计算耗时，不受系统时间校准影响。
// 取消后的保存使用独立且有上限的 context，避免丢失已完成操作的记录。
func (s *Span) End(status Status) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Status != Running {
		return
	}
	if status != Completed && status != Failed && status != Cancelled {
		status = Failed
	}
	elapsed := time.Since(s.started).Milliseconds()
	s.record.DurationMS = &elapsed
	s.record.FinishedAt = time.Now().UTC()
	s.record.Status = status
	s.record.Revision++
	s.recorder.publish(s.record)
}

// ResultStatus 把执行边界的错误和取消映射成统一状态，取消优先于普通失败。
func ResultStatus(ctx context.Context, err error) Status {
	if ctx != nil && ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return Cancelled
	}
	if err != nil {
		return Failed
	}
	return Completed
}

// ListAgent 返回指定 Agent 最近的记录；数量限制由存储边界执行。
func (r *Recorder) ListAgent(ctx context.Context, agentID string, limit int) ([]Record, error) {
	return r.store.ListAgent(ctx, agentID, limit)
}

func (r *Recorder) publish(record Record) {
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
