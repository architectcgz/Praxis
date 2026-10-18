// Package contracts 定义 Agent 应用入口的公开数据与瞬时通知。
package contracts

// AgentDetail 只保留调用方可展示的 Agent 状态。
type AgentDetail struct {
	ID                string
	Name              string
	SessionID         string
	DefinitionID      string
	Profile           string
	State             string
	CurrentTaskID     string
	TaskIDs           []string
	Tasks             []TaskInfo
	ControlCommandIDs []string
}
