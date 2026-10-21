// Package jsonl 提供可靠追加、文件寻址和启动 Replay。
package jsonl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/utils/pathutil"
	"strings"
)

// Log 由 Storage 在进程内互斥锁下串行访问；只保存日志资源和文件游标。
type Log struct {
	root            string
	projects        string
	closed          bool
	fault           error
	sequences       map[string]uint64
	lengths         map[string]int64
	sessionProjects map[string]string
}

type Scope struct {
	ID        string
	ProjectID string
}

// Open 初始化日志资源；调用方必须在日志关闭前持续持有应用独占运行权。
func Open(ctx context.Context, root string) (*Log, error) {
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
	s := &Log{
		root:            state,
		projects:        projects,
		sequences:       make(map[string]uint64),
		lengths:         make(map[string]int64),
		sessionProjects: make(map[string]string),
	}
	return s, nil
}
func (s *Log) Scopes(ctx context.Context) ([]Scope, error) {
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
		if !ValidScope(project.Name()) {
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
			if !ValidScope(scope) {
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

func ValidScope(scope string) bool {
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

func (s *Log) Path(scope, projectID string) (string, error) {
	if scope == "" {
		return filepath.Join(s.root, "projects.jsonl"), nil
	}
	if !ValidScope(scope) {
		return "", errors.New("session ID is not a valid file name")
	}
	if known := s.sessionProjects[scope]; known != "" {
		if projectID != "" && projectID != known {
			return "", errors.New("log project ownership mismatch")
		}
		projectID = known
	}
	if !ValidScope(projectID) {
		return "", errors.New("session project ID is not a valid directory name")
	}
	return filepath.Join(s.projects, projectID, "sessions", scope+".jsonl"), nil
}

func (s *Log) Sequence(scope string) uint64 { return s.sequences[scope] }
func (s *Log) Project(scope string) string  { return s.sessionProjects[scope] }

func (s *Log) Check() error {
	if s.closed {
		return errors.New("log is closed")
	}
	return s.fault
}

// ExistingPath 仅供显式导出日志的业务使用，普通实体查询不调用文件系统。
func (s *Log) ExistingPath(scope string) (string, error) {
	path, err := s.Path(scope, "")
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
	s.closed = true
	return nil
}
