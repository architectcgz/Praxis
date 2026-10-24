// Package identity 定义内核使用的强类型标识，不承载应用层公开契约。
package identity

// ID 使用不同类型，防止仓储意外交换不同聚合的标识符，
// 同时允许所有持久化格式继续使用字符串。
type (
	ProjectID         string
	WorkspaceID       string
	SessionID         string
	AgentDefinitionID string
	AgentID           string
	AgentExecutionID  string
	TaskID            string
	TurnID            string
	RequestID         string
	WaitConditionID   string
)

func (id ProjectID) String() string         { return string(id) }
func (id WorkspaceID) String() string       { return string(id) }
func (id SessionID) String() string         { return string(id) }
func (id AgentDefinitionID) String() string { return string(id) }
func (id AgentID) String() string           { return string(id) }
func (id AgentExecutionID) String() string  { return string(id) }
func (id TurnID) String() string            { return string(id) }
func (id RequestID) String() string         { return string(id) }
func (id WaitConditionID) String() string   { return string(id) }
func (id TaskID) String() string            { return string(id) }
