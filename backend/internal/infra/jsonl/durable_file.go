package jsonl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// 新文件先完整同步，再以不覆盖方式发布，首次提交不会暴露半条记录。
func writeNewFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".commit-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return publishFile(tmp, path)
}

// createSession 只用于显式创建，首条记录可靠发布后才登记文件游标。
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
	for _, directory := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(directory)); err != nil {
			return err
		}
	}
	if err := writeNewFile(path, data); err != nil {
		if !errors.Is(err, os.ErrExist) {
			// 发布结果可能不明，禁止继续追加；重开日志后确认实际结果。
			s.fault = fmt.Errorf("initial log commit failed; restart required: %w", err)
		}
		return fmt.Errorf("create session log %s: %w", path, err)
	}
	s.advance(sessionID, first, int64(len(data)))
	s.sessionProjects[sessionID] = projectID
	return nil
}

// deleteSession 先删除并同步目录，再释放游标；结果不明时停止访问，重启后以文件恢复状态。
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
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		s.fault = fmt.Errorf("session log deletion outcome is unknown; restart required: %w", err)
		return s.fault
	}
	delete(s.sequences, sessionID)
	delete(s.lengths, sessionID)
	delete(s.sessionProjects, sessionID)
	return nil
}

// appendExisting 不创建文件，也不预查存在性；打开和写入错误直接返回调用方。
func (s *Log) appendExisting(path string, offset int64, data []byte) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", path, err)
	}
	defer file.Close()
	return s.appendTo(file, offset, data)
}

// logFile 仅描述可靠追加与回滚所需的文件操作，便于覆盖实际 I/O 失败边界。
type logFile interface {
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Truncate(int64) error
}

func (s *Log) appendTo(file logFile, offset int64, data []byte) error {
	n, err := file.WriteAt(data, offset)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		rollback := errors.Join(file.Truncate(offset), file.Sync())
		if rollback != nil {
			s.fault = fmt.Errorf("log write outcome is unknown; restart required: %w", errors.Join(err, rollback))
		}
		return errors.Join(err, rollback)
	}
	return nil
}
