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
	path, err := s.ExistingPath(sessionID)
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

// appendExisting 只顺序追加，不回滚失败写入、不逐条 fsync。
// 写入失败后调用方必须停止后续写入，避免越过不完整尾行继续生成日志。
func (s *Log) appendExisting(path string, offset int64, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", path, err)
	}
	n, writeErr := file.WriteAt(data, offset)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}
