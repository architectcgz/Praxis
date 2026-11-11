package session

import (
	"praxis/internal/clock"
	"praxis/internal/identity"
	"slices"
	"sync"
)

// Session 持有会话数据；提交锁保证接纳与同步追加顺序，数据锁只保护内存。
// 日志写入失败不撤销内存更新；发布后的 Entry 及其内部载荷只读。
// 模型请求、工具执行和 Hook 不持有会话锁。
type Session struct {
	commitMu sync.Mutex
	mu       sync.RWMutex
	data     *Data
	log      EventLog
	clock    clock.Clock
	ids      identity.IDGenerator
}

// New 接管完成索引与必要恢复的数据；发布实例后不再直接修改 data。
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
	for _, branch := range s.data.Branches {
		if branch.HeadEntryID != "" {
			return false
		}
	}
	return true
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
