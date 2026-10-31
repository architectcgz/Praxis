package storage

import (
	"context"
	"fmt"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/repository"
)

// SessionLog 只负责历史文件读写；同一会话的加载和追加由业务层串行调用。
type SessionLog struct{ sessions SessionRepository }

var _ repository.SessionLog = SessionLog{}

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

// Append 立即写入完整记录，不逐条 fsync；写入失败由业务调用方处理。
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
