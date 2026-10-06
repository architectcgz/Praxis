package jsonl

import (
	"context"
	"errors"
	"os"

	"praxis/internal/contracts"
)

// SessionLogPath 返回现存会话的 JSONL 绝对路径；非法 ID、已删除会话和缺失文件均返回错误。
func (s *Store) SessionLogPath(ctx context.Context, sessionID contracts.SessionID) (string, error) {
	if !validScope(sessionID.String()) {
		return "", contracts.InvalidValue("sessionId", "Session ID 不是合法文件名")
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
		return "", contracts.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("会话日志必须是普通文件")
	}
	return path, nil
}
