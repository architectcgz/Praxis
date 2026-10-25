package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/infra/jsonl"
	"slices"
	"sync"
)

// Storage 拥有进程内完整记录和有界写入队列；业务成功只表示内存已接纳，不承诺落盘。
// 所有仓储共享这把锁，后台写入不持锁执行文件操作。
type Storage struct {
	mu             sync.Mutex
	records        []logRecord
	positions      map[string]int
	sequences      map[string]uint64
	projects       map[string]string
	log            *jsonl.Log
	pending        chan persistenceOperation
	done           chan struct{}
	closed         bool
	persistenceErr error
	report         func(error)
}

// Open 一次性重放全部日志并解码为业务值，完成后才启动顺序写入者。
// report 只报告首次持久化退化，调用方应将错误写入应用日志。
func Open(ctx context.Context, root string, report func(error)) (*Storage, error) {
	s := &Storage{
		positions: make(map[string]int),
		sequences: make(map[string]uint64),
		projects:  make(map[string]string),
		pending:   make(chan persistenceOperation, 1024),
		done:      make(chan struct{}),
		report:    report,
	}
	log, err := jsonl.Open(ctx, root, func(scope jsonl.Scope, event jsonl.Event) error {
		value, err := decodeStoredEvent(scope.ID, event)
		if err != nil {
			return err
		}
		key := recordKey(scope.ID, event.Collection, event.Key)
		if index, ok := s.positions[key]; ok {
			before, err := json.Marshal(s.records[index].value)
			if err != nil {
				return err
			}
			if err := immutable(event.Collection, before, event.Payload); err != nil {
				return err
			}
		}
		s.remember(scope.ID, event, value)
		if scope.ID != "" {
			s.projects[scope.ID] = scope.ProjectID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.log = log
	go s.persist()
	return s, nil
}

func recordKey(scope, collection, id string) string {
	return scope + "\x00" + collection + "\x00" + id
}

// remember 只保存独立业务值，编码正文由待写队列持有，避免长期保留两份正文。
func (s *Storage) remember(scope string, event jsonl.Event, value any) {
	key := recordKey(scope, event.Collection, event.Key)
	event.Payload = nil
	record := logRecord{
		scope: scope,
		event: event,
		value: value,
	}
	if index, ok := s.positions[key]; ok {
		s.records[index] = record
	} else {
		s.positions[key] = len(s.records)
		s.records = append(s.records, record)
	}
	s.sequences[scope] = event.Sequence
}

func (s *Storage) forgetSession(scope string) {
	s.records = slices.DeleteFunc(s.records, func(record logRecord) bool { return record.scope == scope })
	clear(s.positions)
	for index, record := range s.records {
		s.positions[recordKey(record.scope, record.event.Collection, record.event.Key)] = index
	}
	delete(s.sequences, scope)
	delete(s.projects, scope)
}

// withState 只访问内存；回调不得重入仓储、执行文件操作或调用外部业务。
func withState(ctx context.Context, s *Storage, action func(*Storage, []logRecord) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.closed {
		s.mu.Unlock()
		return errors.New("storage is closed")
	}
	prior := s.persistenceErr
	err := action(s, s.records)
	failure := s.persistenceErr
	s.mu.Unlock()
	if prior == nil && failure != nil && s.report != nil {
		s.report(failure)
	}
	return err
}

// PersistenceError 返回本次运行的持久化退化原因；内存查询和修改不受该错误影响。
func (s *Storage) PersistenceError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistenceErr
}

// Close 停止接纳并尽力排空已入队记录；超时不释放仍在写入的日志，可再次等待关闭。
func (s *Storage) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.pending)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return s.PersistenceError()
	case <-ctx.Done():
		return fmt.Errorf("wait for storage persistence: %w", ctx.Err())
	}
}
