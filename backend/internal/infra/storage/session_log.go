package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
)

// SessionLogPath 返回现存会话的 JSONL 绝对路径；非法 ID、已删除会话和缺失文件均返回错误。
func (s *Store) SessionLogPath(ctx context.Context, sessionID identity.SessionID) (string, error) {
	if !jsonl.ValidScope(sessionID.String()) {
		return "", fmt.Errorf("sessionId: session ID is not a valid file name: %w", errorcode.ErrInvalidValue)
	}
	var path string
	err := s.view(ctx, func() error {
		if _, err := getLocked[sessionmodel.Session](s, "session", sessionID.String()); err != nil {
			return err
		}
		var err error
		path, err = s.log.ExistingPath(sessionID.String())
		if errors.Is(err, os.ErrNotExist) {
			return errorcode.ErrNotFound
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return path, nil
}
