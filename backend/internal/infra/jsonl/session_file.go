package jsonl

import (
	"context"
	"errors"
	"os"
	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
)

// SessionLogPath 返回现存会话的 JSONL 绝对路径；非法 ID、已删除会话和缺失文件均返回错误。
func (s *Store) SessionLogPath(ctx context.Context, sessionID identity.SessionID) (string, error) {
	if !validScope(sessionID.String()) {
		return "", fault.InvalidValue("sessionId", "session ID is not a valid file name")
	}
	if _, err := (SessionRepository{s}).Get(ctx, sessionID); err != nil {
		return "", err
	}
	path, err := s.path(sessionID.String())
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fault.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("session log must be a regular file")
	}
	return path, nil
}
