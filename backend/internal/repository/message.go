package repository

import (
	"context"
	"praxis/internal/core/message"
)

// MessagePage 的消息与序号边界来自同一次一致性读取。
type MessagePage struct {
	SequenceBoundary uint64
	Messages         []message.Message
}

type MessageQuery struct {
	Stream message.StreamKey
	After  uint64
	Limit  int
	Tail   bool
	// IncludeUnadmitted 供准入冻结排除列表；普通展示查询隐藏尚未提交 Execution/Task 的输入。
	IncludeUnadmitted bool
}

// Message 只存取显式指定流的消息，不选择流或更新 Execution。
type Message interface {
	Append(context.Context, message.Record) (message.Message, bool, error)
	Get(context.Context, message.StreamKey, string) (message.Message, error)
	List(context.Context, MessageQuery) (MessagePage, error)
}
