package storage

import (
	"context"
	"errors"
	"os"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
)

// SessionLogPath 返回现存会话的 JSONL 绝对路径；非法 ID、已删除会话和缺失文件均返回错误。
func (s *Store) SessionLogPath(ctx context.Context, sessionID identity.SessionID) (string, error) {
	if !jsonl.ValidScope(sessionID.String()) {
		return "", fault.InvalidValue("sessionId", "session ID is not a valid file name")
	}
	var path string
	err := s.view(ctx, func(readCtx context.Context) error {
		if _, err := (SessionRepository{s}).Get(readCtx, sessionID); err != nil {
			return err
		}
		var err error
		path, err = s.log.ExistingPath(sessionID.String())
		if errors.Is(err, os.ErrNotExist) {
			return fault.ErrNotFound
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return path, nil
}
