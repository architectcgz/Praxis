package contracts

import (
	"encoding/json"
	"time"
)

// AgentMessage 返回过滤后的公开消息及结构化内容。
type AgentMessage struct {
	ID         string
	Sequence   uint64
	At         time.Time
	TaskID     string
	Role       string
	AuthorKind string
	AuthorID   string
	Content    string
	Thinking   string
	Blocks     []MessageBlock
}

// MessageBlock 只包含调用方可展示的内容块。
type MessageBlock struct {
	Kind    string
	Text    string
	CallID  string
	Name    string
	Input   json.RawMessage
	IsError bool
}

// AgentHistoryPage 返回合并后的消息和任务终态，以及向前翻页的稳定游标。
type AgentHistoryPage struct {
	Items      []AgentHistoryItem
	NextCursor string
}

// AgentHistoryItem 表示公开消息或需要展示的任务终态。
type AgentHistoryItem struct {
	Kind     string
	Sequence uint64
	At       time.Time
	Message  *AgentMessage
	Task     *TaskInfo
}
