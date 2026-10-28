package jsonl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeNewFile 不覆盖已有文件；不执行事务发布或逐条同步，异常退出时由重放处理未完成尾行。
func writeNewFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}

func (s *Log) createSession(ctx context.Context, sessionID, projectID string, first Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.sessionPath(sessionID, projectID)
	if err != nil {
		return err
	}
	data, err := encodeEvent(first)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := writeNewFile(path, data); err != nil {
		return fmt.Errorf("create session log %s: %w", path, err)
	}
	s.advance(sessionID, first, int64(len(data)))
	s.sessionProjects[sessionID] = projectID
	return nil
}

// deleteSession 不等待目录同步，进程异常退出后允许按实际存在的日志恢复。
func (s *Log) deleteSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.existingPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete session log %s: %w", path, err)
	}
	delete(s.sequences, sessionID)
	delete(s.lengths, sessionID)
	delete(s.sessionProjects, sessionID)
	return nil
}

// appendExisting 只追加到已确认的完整前缀，不回滚失败写入、不逐条 fsync。
// 部分写入后长度不再匹配，后续写入直接报错，由下次启动恢复未完成尾行。
func (s *Log) appendExisting(path string, length int64, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	if info.Size() != length {
		return errors.Join(fmt.Errorf("log length mismatch for %s: expected %d, got %d", path, length, info.Size()), file.Close())
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}
