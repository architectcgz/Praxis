package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
	"sync"
)

// Store 持有会话元数据库与按需载入的历史；所有仓储共享同一把读写锁。
type Store struct {
	mu       sync.RWMutex
	dataRoot string
	sessions *sql.DB
	state    state
	closed   bool
	// 由 mu 保护；追加失败的日志须重新打开存储并重放后才能继续写入。
	writeErrors map[string]error
}

// Open 初始化存储目录和项目日志，打开元数据库并恢复项目状态；会话历史在首次访问时载入。
func Open(ctx context.Context, dataRoot string) (*Store, error) {
	s := &Store{
		dataRoot:    dataRoot,
		state:       newState(),
		writeErrors: make(map[string]error),
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(s.dataRoot, "projects"), 0700); err != nil {
		return nil, err
	}
	// 项目汇总日志随存储初始化建立，普通追加不承担首次创建。
	if err := checkRegularFile(s.projectLogPath()); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := createLog(ctx, s.projectLogPath()); err != nil {
			return nil, err
		}
	}
	if err := s.replayLog(ctx, s.projectLogPath(), logScope{}); err != nil {
		return nil, err
	}
	db, err := openSessionDatabase(ctx, dataRoot)
	if err != nil {
		return nil, err
	}
	s.sessions = db
	return s, nil
}

func (s *Store) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return err
	}
	return nil
}

// lockSession 在同一把写锁内完成首次重放，避免并发访问重复载入或看到半成品状态。
func (s *Store) lockSession(ctx context.Context, id identity.SessionID) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	if err := s.loadSessionLocked(ctx, id); err != nil {
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) readLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	if err := s.check(ctx); err != nil {
		s.mu.RUnlock()
		return err
	}
	return nil
}

func (s *Store) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return errors.New("storage is closed")
	}
	return nil
}

// appendEventLocked 只在编码边界接收任意载荷；成功后仓储直接赋值对应的类型化状态。
func (s *Store) appendEventLocked(ctx context.Context, scope identity.SessionID, typ, collection, key string, value any) error {
	event, err := newEvent(s.state.sequences[scope.String()]+1, typ, collection, key, value)
	if err != nil {
		return err
	}
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	path := s.projectLogPath()
	if scope != "" {
		session, ok := s.state.sessions[scope]
		if !ok {
			return errorcode.ErrNotFound
		}
		path = s.sessionLogPath(scope.String(), session.ProjectID.String())
	}
	if err := s.appendLogLocked(ctx, path, data); err != nil {
		return err
	}
	s.state.sequences[scope.String()] = event.Sequence
	return nil
}

func (s *Store) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.closed = true
	return s.sessions.Close()
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
