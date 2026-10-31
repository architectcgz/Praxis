package repository

import (
	"context"
	"praxis/internal/identity"
)

// SessionLog 读取完整日志行并追加已编码记录，不持有恢复后的业务状态。
// 调用方负责同一会话的串行访问；追加成功不承诺断电持久化。
type SessionLog interface {
	Read(context.Context, identity.SessionID, func([]byte) error) error
	Append(context.Context, identity.SessionID, []byte) error
}
