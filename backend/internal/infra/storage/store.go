package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"slices"
	"sync"
	"time"
)

// Store 先顺序写入日志，再发布内存状态；所有仓储共享同一把读写锁。
type Store struct {
	mu     sync.RWMutex
	state  state
	log    *jsonl.Log
	closed bool
}

// Open 在开放仓储前完成日志重放；运行时查询只读取类型化内存状态。
func Open(ctx context.Context, root string) (*Store, error) {
	s := &Store{state: newState()}
	log, err := jsonl.Open(ctx, root, func(scope jsonl.Scope, event jsonl.Event) error {
		return replayEvent(&s.state, scope, event)
	})
	if err != nil {
		return nil, err
	}
	s.log = log
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
	if scope == "" {
		_, err = s.log.AppendProject(ctx, event)
	} else {
		session, ok := s.state.sessions[scope]
		if !ok {
			return errorcode.ErrNotFound
		}
		_, err = s.log.AppendSession(ctx, scope.String(), session.ProjectID.String(), event)
	}
	if err != nil {
		return err
	}
	s.state.sequences[scope.String()] = event.Sequence
	return nil
}

func newEvent(sequence uint64, typ, collection, key string, value any) (jsonl.Event, error) {
	if sequence == 0 {
		return jsonl.Event{}, errors.New("storage sequence exhausted")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return jsonl.Event{}, err
	}
	return jsonl.Event{
		Sequence:   sequence,
		ID:         fmt.Sprintf("event:%d", sequence),
		Type:       typ,
		Collection: collection,
		Key:        key,
		CreatedAt:  time.Now().UTC(),
		Payload:    data,
	}, nil
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
	if err := s.log.Close(); err != nil {
		return err
	}
	s.closed = true
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
