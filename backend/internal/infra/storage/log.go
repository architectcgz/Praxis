package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// logScope 描述存储目录中的业务归属，不属于通用日志格式。
type logScope struct {
	ID string
}

// appendLogLocked 调用方须持有 Store 写锁；追加 I/O 失败后阻断该日志，避免继续写入未确认的尾部。
func (s *Store) appendLogLocked(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if writeErr := s.writeErrors[path]; writeErr != nil {
		return fmt.Errorf("log %s is not writable; reopen storage to recover: %w", path, writeErr)
	}
	if err := appendExistingLog(path, data); err != nil {
		writeErr := fmt.Errorf("append log %s: %w", path, err)
		s.writeErrors[path] = writeErr
		return writeErr
	}
	return nil
}

func (s *Store) projectLogPath() string {
	return filepath.Join(s.dataRoot, "projects.jsonl")
}

// sessionLogPath 的身份由入口校验，项目归属取自 SQLite 会话元数据。
func (s *Store) sessionLogPath(sessionID, projectID string) string {
	return filepath.Join(s.dataRoot, "projects", projectID, "sessions", sessionID+".jsonl")
}

func checkRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("log must be a regular file")
	}
	return nil
}

// createLog 建立空日志，不覆盖已有文件；成功返回不保证断电持久化。
func createLog(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create log %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("create log %s: %w", path, err)
	}
	return nil
}

// appendExistingLog 不补建文件、不回滚失败写入、不逐条同步；写入与关闭错误交由 Store 阻断后续追加。
func appendExistingLog(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}
