package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// History 提供主会话历史读写及其内存上下文，具体追加和替换操作由 context 模块提供。
type History interface {
	AppendEntry(context.Context, identity.SessionID, sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error)
	UpdateToolResult(context.Context, identity.SessionID, string, string) (sessionentry.ContextEntry, error)
	GetEntry(context.Context, identity.SessionID, string) (sessionentry.ContextEntry, error)
	ListEntries(context.Context, identity.SessionID) (sessionentry.List, error)
	AdmitInput(context.Context, identity.SessionID, string) error
	ContextEntries(context.Context, identity.SessionID) ([]sessionentry.ContextEntry, error)
	ReplaceContext(context.Context, identity.SessionID, []sessionentry.ContextEntry) error
}
