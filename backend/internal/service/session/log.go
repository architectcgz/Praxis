package session

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// LogStore 只读写会话日志的完整行，不解释事件类型或业务载荷。
type LogStore interface {
	Read(context.Context, identity.SessionID, func([]byte) error) error
	Append(context.Context, identity.SessionID, []byte) error
}

// eventLog 在会话侧完成事件编解码，将完整记录交给文件存储。
type eventLog struct {
	store LogStore
}

var _ sessionmodel.EventLog = eventLog{}

func (l eventLog) Read(ctx context.Context, id identity.SessionID, apply func(sessionmodel.Event) error) error {
	return l.store.Read(ctx, id, func(line []byte) error {
		event, err := decodeEvent(line)
		if err != nil {
			return err
		}
		return apply(event)
	})
}

func (l eventLog) AppendEvent(ctx context.Context, id identity.SessionID, event sessionmodel.Event) error {
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	return l.store.Append(ctx, id, data)
}
