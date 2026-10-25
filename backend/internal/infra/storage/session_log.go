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

// SessionLogPath 是显式文件访问，只返回已存在的日志路径，不等待待写队列。
// 新会话尚未写盘或文件已删除时返回不存在；文件内容可能落后于内存。
func (r SessionRepository) SessionLogPath(ctx context.Context, sessionID identity.SessionID) (path string, err error) {
	if !jsonl.ValidScope(sessionID.String()) {
		return "", fmt.Errorf("sessionId: invalid log file name: %w", errorcode.ErrInvalidValue)
	}
	err = withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		_, err := getRecord[sessionmodel.Session](records, "session", sessionID.String())
		return err
	})
	if err != nil {
		return "", err
	}
	err = r.storage.log.With(ctx, func(access *jsonl.Access) error {
		path, err = access.ExistingPath(sessionID.String())
		if errors.Is(err, os.ErrNotExist) {
			return errorcode.ErrNotFound
		}
		return err
	})
	return path, err
}
