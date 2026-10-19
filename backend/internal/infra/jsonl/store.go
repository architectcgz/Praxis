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
	mu              sync.Mutex
	root            string
	projects        string
	lock            *os.File
	closed          bool
	fault           error
	sequences       map[string]uint64
	lengths         map[string]int64
	sessionProjects map[string]string
	index           *index
	messages        messageCache
}

// Open 接收产品数据根目录；运行资源位于 state，会话日志按项目归档。
func Open(ctx context.Context, root string) (*Store, error) {
	if ctx == nil || !pathutil.IsAbsoluteNormalized(root) {
		return nil, errors.New("log context and canonical absolute directory are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state := filepath.Join(root, "state")
	projects := filepath.Join(root, "projects")
	for _, directory := range []string{state, projects} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
	}
	for _, directory := range []string{root, filepath.Dir(root)} {
		if err := syncDirectory(directory); err != nil {
			return nil, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(state, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("data directory is already in use: %w", err)
	}
	s := &Store{
		root:            state,
		projects:        projects,
		lock:            lock,
		sequences:       make(map[string]uint64),
		lengths:         make(map[string]int64),
		sessionProjects: make(map[string]string),
		index:           newIndex(),
		messages:        newMessageCache(4096),
	}
	if err = s.replay(ctx, ""); err == nil {
		err = s.replaySessions(ctx)
	}
	if err != nil {
		_ = s.Close(context.Background())
		return nil, err
	}
	return s, nil
}

func (s *Store) replaySessions(ctx context.Context) error {
	projects, err := os.ReadDir(s.projects)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !project.IsDir() {
			continue
		}
		if !validScope(project.Name()) {
			return fmt.Errorf("invalid project directory name: %s", project.Name())
		}
		directory := filepath.Join(s.projects, project.Name(), "sessions")
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("session logs must be in a directory: %s", directory)
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			scope, ok := strings.CutSuffix(entry.Name(), ".jsonl")
			if !ok {
				continue
			}
			if !validScope(scope) {
				return fmt.Errorf("invalid log file name: %s", entry.Name())
			}
			if _, exists := s.sessionProjects[scope]; exists {
				return fmt.Errorf("session log belongs to multiple projects: %s", scope)
			}
			s.sessionProjects[scope] = project.Name()
			if err := s.replay(ctx, scope); err != nil {
				return err
			}
		}
	}
	return nil
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

func (s *Store) path(ctx context.Context, scope string) (string, error) {
	if scope == "" {
		return filepath.Join(s.root, "projects.jsonl"), nil
	}
	if !validScope(scope) {
		return "", errors.New("session ID is not a valid file name")
	}
	projectID := s.sessionProjects[scope]
	if projectID == "" {
		// 首次提交前只读取事务内的会话归属，不提前发布全局索引。
		entry, _ := s.entry(ctx, entityKey{"session", scope})
		for _, group := range entry.groups {
			if group.kind == "sessions-project" {
				projectID = group.owner
				break
			}
		}
	}
	if !validScope(projectID) {
		return "", errors.New("session project ID is not a valid directory name")
	}
	return filepath.Join(s.projects, projectID, "sessions", scope+".jsonl"), nil
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
