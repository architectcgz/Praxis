package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// appendEvent 只追加已有会话日志；序号和身份已由持锁入口分配。
func (s *Log) appendEvent(ctx context.Context, sessionID, projectID string, event Event) error {
	path, err := s.sessionPath(sessionID, projectID)
	if err != nil {
		return err
	}
	if err := s.appendRecord(ctx, path, sessionID, event); err != nil {
		return err
	}
	s.sessionProjects[sessionID] = projectID
	return nil
}

func (s *Log) appendProjectEvent(ctx context.Context, event Event) error {
	return s.appendRecord(ctx, s.projectPath(), "", event)
}

func (s *Log) appendRecord(ctx context.Context, path, fileKey string, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := encodeEvent(event)
	if err != nil {
		return err
	}
	if err := s.appendExisting(path, s.lengths[fileKey], encoded); err != nil {
		return err
	}
	s.advance(fileKey, event, int64(len(encoded)))
	return nil
}

func encodeEvent(event Event) ([]byte, error) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func (s *Log) advance(fileKey string, event Event, length int64) {
	s.sequences[fileKey] = event.Sequence
	s.lengths[fileKey] += length
}

// read 以调用内序号扫描日志，不持有解码结果；未完成尾行备份并截断，完整坏行报错。
func (s *Log) read(ctx context.Context, scope Scope, apply func(Event) error) error {
	path := s.projectPath()
	if scope.ID != "" {
		var err error
		path, err = s.sessionPath(scope.ID, scope.ProjectID)
		if err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var offset int64
	var sequence uint64
	finish := func() {
		s.sequences[scope.ID] = sequence
		s.lengths[scope.ID] = offset
		if scope.ID != "" {
			s.sessionProjects[scope.ID] = scope.ProjectID
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				if err := s.truncateTail(file, offset, line); err != nil {
					return err
				}
			}
			finish()
			return nil
		}
		if readErr != nil {
			return readErr
		}
		var event Event
		// 严格解码完整行，未知结构必须报错，不能忽略已存在的历史。
		if err := Decode(line, &event); err != nil {
			return fmt.Errorf("%s offset %d: expected a single JSONL event: %w", path, offset, err)
		}
		if sequence == ^uint64(0) || event.Sequence != sequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
			return fmt.Errorf("%s offset %d: invalid event identity or sequence", path, offset)
		}
		if apply != nil {
			if err := apply(event); err != nil {
				return fmt.Errorf("%s offset %d: %w", path, offset, err)
			}
		}
		sequence = event.Sequence
		offset += int64(len(line))
	}
}

func (s *Log) truncateTail(file *os.File, offset int64, tail []byte) error {
	backup, err := os.CreateTemp(s.root, "incomplete-tail-*")
	if err != nil {
		return err
	}
	_, writeErr := backup.Write(tail)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		return err
	}
	if err := syncDirectory(s.root); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}
