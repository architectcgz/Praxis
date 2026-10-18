// Package jsonl 实现单进程本地日志、原子事务和可重建的位置索引。
package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/utils/pathutil"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Sequence   uint64          `json:"seq"`
	ID         string          `json:"event_id"`
	Type       string          `json:"type"`
	Collection string          `json:"collection"`
	Key        string          `json:"key"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// 一行是一笔原子提交，位置引用必须同时包含行位置和事件下标。
type batch struct {
	Events []Event `json:"events"`
}

type Store struct {
	mu        sync.Mutex
	root      string
	lock      *os.File
	closed    bool
	fault     error
	sequences map[string]uint64
	lengths   map[string]int64
	index     *index
	messages  messageCache
}

func Open(ctx context.Context, root string) (*Store, error) {
	if ctx == nil || !pathutil.IsAbsoluteNormalized(root) {
		return nil, errors.New("log context and canonical absolute directory are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Dir(root)); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("data directory is already in use: %w", err)
	}
	s := &Store{
		root:      root,
		lock:      lock,
		sequences: make(map[string]uint64),
		lengths:   make(map[string]int64),
		index:     newIndex(),
		messages:  newMessageCache(4096),
	}
	err = os.MkdirAll(filepath.Join(root, "sessions"), 0700)
	if err == nil {
		err = syncDirectory(root)
	}
	if err == nil {
		err = s.replay(ctx, "")
	}
	if err == nil {
		var entries []os.DirEntry
		entries, err = os.ReadDir(filepath.Join(root, "sessions"))
		for _, entry := range entries {
			if err != nil {
				break
			}
			if strings.HasSuffix(entry.Name(), ".jsonl") {
				scope := strings.TrimSuffix(entry.Name(), ".jsonl")
				if !validScope(scope) {
					err = fmt.Errorf("invalid log file name: %s", entry.Name())
					break
				}
				err = s.replay(ctx, scope)
			}
		}
	}
	if err != nil {
		_ = s.Close(context.Background())
		return nil, err
	}
	return s, nil
}

func validScope(scope string) bool {
	if scope == "" || len(scope) > 180 || !filepath.IsLocal(scope) {
		return false
	}
	for _, c := range scope {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) path(scope string) (string, error) {
	if scope == "" {
		return filepath.Join(s.root, "projects.jsonl"), nil
	}
	if !validScope(scope) {
		return "", errors.New("session ID is not a valid file name")
	}
	return filepath.Join(s.root, "sessions", scope+".jsonl"), nil
}

func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("close context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.lock.Close()
}
