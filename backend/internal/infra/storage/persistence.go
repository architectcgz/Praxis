package storage

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/infra/jsonl"
)

type persistenceKind uint8

const (
	persistAppend persistenceKind = iota
	persistCreateSession
	persistDeleteSession
)

type persistenceOperation struct {
	kind      persistenceKind
	scope     string
	projectID string
	event     jsonl.Event
}

// enqueue 必须持有内存锁；队列满不阻塞业务，也不允许后续记录越过缺口写入日志。
// 已接纳的有序前缀仍会尽力写完，后续内存状态持续可用，但不再声称能够保存。
func (s *Storage) enqueue(operation persistenceOperation) {
	if s.persistenceErr != nil {
		return
	}
	select {
	case s.pending <- operation:
	default:
		s.persistenceErr = errors.New("persistence queue is full; subsequent changes are memory-only until restart")
	}
}

func (s *Storage) persistenceFailed(err error) {
	s.mu.Lock()
	first := s.persistenceErr == nil
	if first {
		s.persistenceErr = fmt.Errorf("persistence stopped; subsequent changes are memory-only until restart: %w", err)
	}
	failure := s.persistenceErr
	s.mu.Unlock()
	if first && s.report != nil {
		s.report(failure)
	}
}

// persist 是唯一文件写入者，严格保持跨仓储操作顺序。
// 失败后不自动重试结果不明的操作，不再写入后续记录；关闭时释放队列正文和日志资源。
func (s *Storage) persist() {
	defer close(s.done)
	failed := false
	for operation := range s.pending {
		if failed {
			continue
		}
		err := s.log.With(context.Background(), func(access *jsonl.Access) error {
			switch operation.kind {
			case persistCreateSession:
				_, err := access.CreateSession(operation.scope, operation.projectID, operation.event)
				return err
			case persistDeleteSession:
				return access.DeleteSession(operation.scope)
			default:
				if operation.scope == "" {
					_, err := access.AppendProject(operation.event)
					return err
				}
				_, err := access.AppendSession(operation.scope, operation.projectID, operation.event)
				return err
			}
		})
		if err != nil {
			failed = true
			s.persistenceFailed(fmt.Errorf("%s/%s/%s: %w", operation.scope, operation.event.Collection, operation.event.Key, err))
		}
	}
	if err := s.log.Close(); err != nil {
		s.persistenceFailed(err)
	}
}
