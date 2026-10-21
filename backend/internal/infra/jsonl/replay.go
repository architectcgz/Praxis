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

// Append 先完成可靠追加，再推进日志游标；实体发布由调用方完成。
func (s *Log) Append(ctx context.Context, scope, projectID string, commit Commit) error {
	if err := s.Check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(commit.Events) == 0 {
		return errors.New("empty commit")
	}
	if err := s.validateSequence(scope, commit); err != nil {
		return err
	}
	encoded, err := json.Marshal(commit)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := s.append(ctx, scope, projectID, encoded); err != nil {
		return err
	}
	s.advance(scope, projectID, commit, int64(len(encoded)))
	return nil
}

func (s *Log) validateSequence(scope string, commit Commit) error {
	sequence := s.sequences[scope]
	for _, event := range commit.Events {
		if sequence == ^uint64(0) || event.Sequence != sequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
			return errors.New("invalid event identity or sequence")
		}
		sequence = event.Sequence
	}
	return nil
}

func (s *Log) advance(scope, projectID string, commit Commit, length int64) {
	s.sequences[scope] = commit.Events[len(commit.Events)-1].Sequence
	s.lengths[scope] += length
	if scope != "" && projectID != "" {
		s.sessionProjects[scope] = projectID
	}
}

// Replay 按行解码并调用投影函数；未完成的尾行备份并截断，完整坏行直接报错。
func (s *Log) Replay(ctx context.Context, scope Scope, apply func(Commit) error) error {
	path, err := s.Path(scope.ID, scope.ProjectID)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("log must be a regular file: %s", path)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				return s.truncateTail(file, offset, line)
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
		var commit Commit
		if err := Decode(line, &commit); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		if len(commit.Events) == 0 {
			return fmt.Errorf("%s offset %d: empty commit", path, offset)
		}
		if err := s.validateSequence(scope.ID, commit); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		if err := apply(commit); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		s.advance(scope.ID, scope.ProjectID, commit, int64(len(line)))
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
