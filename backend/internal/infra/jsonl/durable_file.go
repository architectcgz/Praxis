package jsonl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// 新文件先完整同步，再原子发布，首次提交不会暴露半个目录项或标识文件。
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

func (s *Store) append(ctx context.Context, scope string, data []byte) error {
	path, err := s.path(ctx, scope)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && s.lengths[scope] == 0 {
		if scope != "" {
			// 项目和会话目录按首次有效提交创建，并同步父目录以保证发布可恢复。
			for _, directory := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
				if err := os.MkdirAll(directory, 0700); err != nil {
					return err
				}
				if err := syncDirectory(filepath.Dir(directory)); err != nil {
					return err
				}
			}
		}
		if err := writeNewFile(path, data); err != nil {
			// 发布结果不明时不再基于旧索引写入，重启后以日志为准。
			if _, statErr := os.Lstat(path); statErr == nil {
				s.fault = fmt.Errorf("initial log commit outcome is unknown; restart required: %w", err)
			}
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != s.lengths[scope] {
		s.fault = errors.New("log file type or length changed externally; restart required for recovery")
		return s.fault
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return s.appendTo(file, s.lengths[scope], data)
}

// logFile 仅描述可靠追加与回滚所需的文件操作，便于覆盖实际 I/O 失败边界。
type logFile interface {
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Truncate(int64) error
}

func (s *Store) appendTo(file logFile, offset int64, data []byte) error {
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
