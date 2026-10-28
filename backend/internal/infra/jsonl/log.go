// Package jsonl 提供顺序追加、文件寻址和启动重放，不持有业务状态。
package jsonl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/identity"
	"praxis/internal/utils/pathutil"
	"strings"
	"sync"
)

// Log 拥有共享文件锁和日志游标，不保存业务实体或查询投影。
type Log struct {
	mu              sync.Mutex
	root            string
	projects        string
	closed          bool
	sequences       map[string]uint64
	lengths         map[string]int64
	sessionProjects map[string]string
}

type Scope struct {
	ID        string
	ProjectID string
}

// Open 初始化日志资源；调用方必须在日志关闭前持续持有应用独占运行权。
func Open(ctx context.Context, root string, visit func(Scope, Event) error) (*Log, error) {
	if !pathutil.IsAbsoluteNormalized(root) {
		return nil, errors.New("canonical absolute log directory is required")
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
	s := &Log{
		root:            state,
		projects:        projects,
		sequences:       make(map[string]uint64),
		lengths:         make(map[string]int64),
		sessionProjects: make(map[string]string),
	}
	// 项目汇总日志随资源初始化建立，追加路径不承担首次创建。
	if err := writeNewFile(s.projectPath(), nil); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	apply := func(scope Scope) func(Event) error {
		if visit == nil {
			return nil
		}
		return func(event Event) error { return visit(scope, event) }
	}
	if err := s.read(ctx, Scope{}, apply(Scope{})); err != nil {
		return nil, err
	}
	scopes, err := s.scopes(ctx)
	if err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		if err := s.read(ctx, scope, apply(scope)); err != nil {
			return nil, err
		}
		if s.sequences[scope.ID] == 0 {
			if err := s.deleteSession(ctx, scope.ID); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}
func (s *Log) scopes(ctx context.Context) ([]Scope, error) {
	result := make([]Scope, 0)
	seen := make(map[string]bool)
	projects, err := os.ReadDir(s.projects)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !project.IsDir() {
			continue
		}
		if !identity.ProjectID(project.Name()).Valid() {
			return nil, fmt.Errorf("invalid project directory name: %s", project.Name())
		}
		directory := filepath.Join(s.projects, project.Name(), "sessions")
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("session logs must be in a directory: %s", directory)
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			scope, ok := strings.CutSuffix(entry.Name(), ".jsonl")
			if !ok {
				continue
			}
			if !identity.SessionID(scope).Valid() {
				return nil, fmt.Errorf("invalid log file name: %s", entry.Name())
			}
			if seen[scope] {
				return nil, fmt.Errorf("session log belongs to multiple projects: %s", scope)
			}
			seen[scope] = true
			result = append(result, Scope{ID: scope, ProjectID: project.Name()})
		}
	}
	return result, nil
}

func (s *Log) projectPath() string {
	return filepath.Join(s.root, "projects.jsonl")
}

// sessionPath 使用入口已确认可用的身份组装路径，只检查已有日志的项目归属。
func (s *Log) sessionPath(sessionID, projectID string) (string, error) {
	if known := s.sessionProjects[sessionID]; known != "" {
		if projectID != "" && projectID != known {
			return "", errors.New("log project ownership mismatch")
		}
		projectID = known
	}
	return filepath.Join(s.projects, projectID, "sessions", sessionID+".jsonl"), nil
}

func (s *Log) check() error {
	if s.closed {
		return errors.New("log is closed")
	}
	return nil
}

// existingPath 在持有日志锁时检查已有文件。
func (s *Log) existingPath(sessionID string) (string, error) {
	projectID, known := s.sessionProjects[sessionID]
	if !known {
		return "", os.ErrNotExist
	}
	path, err := s.sessionPath(sessionID, projectID)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("log must be a regular file")
	}
	return path, nil
}

func (s *Log) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// CreateSession 同步创建日志，不覆盖已有日志，也不保证断电后已保存。
func (s *Log) CreateSession(ctx context.Context, sessionID, projectID string, first Event) (Event, error) {
	if err := s.lock(ctx); err != nil {
		return Event{}, err
	}
	defer s.mu.Unlock()
	if err := validateNextEvent(0, first); err != nil {
		return Event{}, err
	}
	if err := s.createSession(ctx, sessionID, projectID, first); err != nil {
		return Event{}, err
	}
	return first, nil
}

// DeleteSession 删除会话日志并释放游标，调用方负责协调活动执行和业务状态。
func (s *Log) DeleteSession(ctx context.Context, sessionID string) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	return s.deleteSession(ctx, sessionID)
}

// AppendSession 只追加已有会话日志，文件丢失时返回错误，不负责创建或补建。
func (s *Log) AppendSession(ctx context.Context, sessionID, projectID string, event Event) (Event, error) {
	if err := s.lock(ctx); err != nil {
		return Event{}, err
	}
	defer s.mu.Unlock()
	if err := validateNextEvent(s.sequences[sessionID], event); err != nil {
		return Event{}, err
	}
	if err := s.appendEvent(ctx, sessionID, projectID, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// AppendProject 追加在资源初始化阶段已经建立的项目汇总日志。
func (s *Log) AppendProject(ctx context.Context, event Event) (Event, error) {
	if err := s.lock(ctx); err != nil {
		return Event{}, err
	}
	defer s.mu.Unlock()
	if err := validateNextEvent(s.sequences[""], event); err != nil {
		return Event{}, err
	}
	if err := s.appendProjectEvent(ctx, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// validateNextEvent 不重新分配内存状态确定的序号；不连续时停止写入，而不是掩盖日志缺口。
func validateNextEvent(sequence uint64, event Event) error {
	if sequence == ^uint64(0) {
		return errors.New("log sequence exhausted")
	}
	if event.Sequence != sequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
		return errors.New("invalid event identity or sequence")
	}
	return nil
}

func (s *Log) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := s.check(); err != nil {
		s.mu.Unlock()
		return err
	}
	return nil
}

// ExistingPath 只供显式导出日志使用。
func (s *Log) ExistingPath(ctx context.Context, sessionID string) (string, error) {
	if err := s.lock(ctx); err != nil {
		return "", err
	}
	defer s.mu.Unlock()
	return s.existingPath(sessionID)
}
