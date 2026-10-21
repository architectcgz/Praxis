// Package storage 在可靠日志之上实现事务、内存 State 和实体仓储。
package storage

import (
	"context"
	"errors"
	"praxis/internal/infra/jsonl"
	"sync"
)

type Store struct {
	mu    sync.Mutex
	log   *jsonl.Log
	state *state
}

// Open 在开放业务入口前完整 Replay 全部日志，建立唯一的运行时查询状态。
func Open(ctx context.Context, root string) (*Store, error) {
	log, err := jsonl.Open(ctx, root)
	if err != nil {
		return nil, err
	}
	s := &Store{log: log, state: newState()}
	if err := s.replay(ctx, jsonl.Scope{}); err != nil {
		_ = log.Close()
		return nil, err
	}
	scopes, err := log.Scopes(ctx)
	if err == nil {
		for _, scope := range scopes {
			if err = s.replay(ctx, scope); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = log.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("close context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.log.Close()
}

func (s *Store) project(ctx context.Context, scope string) string {
	if project := s.log.Project(scope); project != "" {
		return project
	}
	entry, _ := s.entry(ctx, entityKey{"session", scope})
	for _, group := range entry.groups {
		if group.kind == "sessions-project" {
			return group.owner
		}
	}
	return ""
}
