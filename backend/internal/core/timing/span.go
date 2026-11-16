package timing

import (
	"sync"
	"time"
)

// Span 只测量耗时，不保存数据或发送事件；nil 表示该操作不计时。
type Span struct {
	mu       sync.Mutex
	record   Record
	started  time.Time
	finished bool
}

func NewSpan() *Span {
	return &Span{record: Record{
		StartedAt: time.Now().UTC(),
	}}
}

// Snapshot 返回只读快照，后续测量只替换字段，不修改已发布的耗时指针。
func (s *Span) Snapshot() *Record {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return new(s.record)
}

// Start 在开始通知和请求准入完成后调用，排除这些操作的开销。
func (s *Span) Start() {
	if s != nil {
		s.started = time.Now()
	}
}

func (s *Span) FirstResponse() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished || s.record.FirstResponseMS != nil {
		return
	}
	s.record.FirstResponseMS = new(time.Since(s.started).Milliseconds())
}

// End 返回单调时钟测量的结果；记录提交由操作的调用方负责。
func (s *Span) End() *Record {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return new(s.record)
	}
	s.record.DurationMS = new(time.Since(s.started).Milliseconds())
	s.record.FinishedAt = time.Now().UTC()
	s.finished = true
	return new(s.record)
}
