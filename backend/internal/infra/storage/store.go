// Package storage 在可靠日志之上实现单事件写入、内存 State 和实体仓储。
package storage

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"sync"
)

type Store struct {
	mu        sync.Mutex
	log       *jsonl.Log
	state     *state
	histories map[identity.AgentID]*agentmodel.History
	pending   []pendingEvent
	sequences map[string]uint64
}

// Open 在开放业务入口前完整 Replay 全部日志，建立唯一的运行时查询状态。
func Open(ctx context.Context, root string) (*Store, error) {
	log, err := jsonl.Open(ctx, root)
	if err != nil {
		return nil, err
	}
	s := &Store{
		log:       log,
		state:     newState(),
		histories: make(map[identity.AgentID]*agentmodel.History),
		sequences: make(map[string]uint64),
	}
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
	s.projectInterruptedTimings()
	return s, nil
}

func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.flushLocked(ctx), s.log.Close())
}

// view 的回调只调用持锁内部函数；同一把锁覆盖条件检查、单事件落盘和索引更新。
func (s *Store) view(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.log.Check(); err != nil {
		return err
	}
	return fn()
}
