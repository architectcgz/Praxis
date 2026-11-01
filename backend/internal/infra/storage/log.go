package storage

import (
	"context"
	"fmt"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
)

// SessionLog 定位会话日志并读写完整行，不解释事件或维护恢复后的业务状态。
type SessionLog struct{ sessions SessionRepository }

func (l SessionLog) Read(ctx context.Context, id identity.SessionID, apply func([]byte) error) error {
	session, err := l.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	path := sessionLogPath(l.sessions.dataRoot, session.ProjectID, id)
	if err := jsonl.CheckRegularFile(path); err != nil {
		return err
	}
	return jsonl.Read(ctx, path, l.sessions.dataRoot, apply)
}

// Append 写入业务侧已经编码的完整行，不逐条 fsync；失败直接返回调用方。
func (l SessionLog) Append(ctx context.Context, id identity.SessionID, data []byte) error {
	session, err := l.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := sessionLogPath(l.sessions.dataRoot, session.ProjectID, id)
	if err := jsonl.Append(path, data); err != nil {
		return fmt.Errorf("append log %s: %w", path, err)
	}
	return nil
}
