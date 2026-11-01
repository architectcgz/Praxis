package session

import (
	"context"
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/repository"
	"slices"
	"sync"
)

// Session 持有配置与完整会话数据；configMu 保护配置及模型选择，mu 保护 data、closed 和 fault。
// mu 的写锁覆盖日志追加与内存推进；模型请求、工具执行和 Hook 不持有会话锁。
type Session struct {
	configMu       sync.RWMutex
	config         *Configuration
	modelSelection sessionmodel.ModelSelection

	mu     sync.RWMutex
	data   *sessionmodel.Data
	log    repository.SessionLog
	closed bool
	fault  error
	failed chan struct{}
}

// checkState 要求调用方持有 mu 的读锁或写锁。
func (s *Session) checkState(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return errors.New("session is closed")
	}
	return s.fault
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

func (s *Session) appendEvent(ctx context.Context, typ, collection, key string, value any) error {
	if s.fault != nil {
		return s.fault
	}
	event, err := newEvent(typ, collection, key, value)
	if err != nil {
		return err
	}
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	if err := s.log.Append(ctx, s.data.ID, data); err != nil {
		if errors.Is(err, errorcode.ErrLogUnavailable) {
			s.fault = fmt.Errorf("session log is unavailable until application restart: %w", err)
			close(s.failed)
		}
		return err
	}
	return nil
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

func (s *Session) Faulted() <-chan struct{} { return s.failed }
