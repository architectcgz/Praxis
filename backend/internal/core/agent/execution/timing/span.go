package timing

import (
	"sync"
	"time"
)

// Span 是调用方持有的计时句柄；互斥保护首响应和唯一一次结束。
type Span struct {
	mu       sync.Mutex
	recorder *Recorder
	record   Record
	started  time.Time
}

// FirstResponse 仅更新内存，首个有效响应耗时随结束快照一起提交。
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
}

// End 用单调时钟测量耗时；结束回调在锁外执行且不计入操作耗时。
func (s *Span) End(status Status) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.record.Status != Running {
		s.mu.Unlock()
		return
	}
	if status != Completed && status != Failed && status != Cancelled {
		status = Failed
	}
	elapsed := time.Since(s.started).Milliseconds()
	s.record.DurationMS = &elapsed
	s.record.FinishedAt = time.Now().UTC()
	s.record.Status = status
	record := s.record
	s.mu.Unlock()
	s.recorder.publish(record)
}
