package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// EntryReader 只提供已经打开的会话条目查询；执行通过绑定的 Session 选择分支路径。
type EntryReader interface {
	ListEntries(context.Context, identity.SessionID, identity.BranchID) (sessionentry.List, error)
}
