package storage

import (
	"context"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
)

// SessionLog 负责会话事件编解码和历史文件读写；不持有恢复后的业务状态。
type SessionLog struct{ sessions SessionRepository }

var _ sessionmodel.EventLog = SessionLog{}

func (l SessionLog) Read(ctx context.Context, id identity.SessionID, apply func(sessionmodel.Event) error) error {
	session, err := l.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	path := sessionLogPath(l.sessions.dataRoot, session.ProjectID, id)
	if err := jsonl.CheckRegularFile(path); err != nil {
		return err
	}
	return jsonl.Read(ctx, path, l.sessions.dataRoot, func(line []byte) error {
		event, err := decodeEvent(line)
		if err != nil {
			return err
		}
		return apply(event)
	})
}

// AppendEvent 立即编码并写入完整记录，不逐条 fsync；失败直接返回调用方。
func (l SessionLog) AppendEvent(ctx context.Context, id identity.SessionID, event sessionmodel.Event) error {
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
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
