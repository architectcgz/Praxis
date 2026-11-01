package session

import (
	"praxis/internal/clock"
	"praxis/internal/identity"
	"slices"
	"sync"
)

// Session 持有完整会话数据；写锁覆盖内存更新与随后的同步日志追加。
// 日志写入失败不撤销内存更新；发布后的 Entry 及其内部载荷只读。
// 模型请求、工具执行和 Hook 不持有会话锁。
type Session struct {
	mu    sync.RWMutex
	data  *Data
	log   EventLog
	clock clock.Clock
	ids   identity.IDGenerator
}

// New 接管已完成日志重放的数据；调用方发布实例后不再直接修改 data。
func New(data *Data, log EventLog, clock clock.Clock, ids identity.IDGenerator) *Session {
	return &Session{
		data:  data,
		log:   log,
		clock: clock,
		ids:   ids,
	}
}

func (s *Session) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Entries) == 0
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}

func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
