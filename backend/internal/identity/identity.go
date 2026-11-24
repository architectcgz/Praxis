// Package identity 定义内核使用的强类型标识，不承载应用层公开契约。
package identity

import "github.com/google/uuid"

// ID 使用不同类型，防止不同聚合或执行层次的标识符混用，
// 同时允许所有持久化格式继续使用字符串。
type (
	ProjectID         string
	SessionID         string
	AgentDefinitionID string
	AgentID           string
	RunID             string
	TaskID            string
	TurnID            string
	ModelRequestID    string
	EntryID           string
	ReportID          string
	OperationID       string
)

func (id ProjectID) String() string         { return string(id) }
func (id SessionID) String() string         { return string(id) }
func (id AgentDefinitionID) String() string { return string(id) }
func (id AgentID) String() string           { return string(id) }
func (id RunID) String() string             { return string(id) }
func (id TaskID) String() string            { return string(id) }
func (id TurnID) String() string            { return string(id) }
func (id ModelRequestID) String() string    { return string(id) }
func (id EntryID) String() string           { return string(id) }
func (id ReportID) String() string          { return string(id) }
func (id OperationID) String() string       { return string(id) }

func newPrefixedID(prefix string) string {
	return prefix + "_" + uuid.Must(uuid.NewV7()).String()
}

func NewAgentID() AgentID               { return AgentID(newPrefixedID("agent")) }
func NewRunID() RunID                   { return RunID(newPrefixedID("run")) }
func NewTurnID() TurnID                 { return TurnID(newPrefixedID("turn")) }
func NewModelRequestID() ModelRequestID { return ModelRequestID(newPrefixedID("request")) }
func NewEntryID() EntryID               { return EntryID(newPrefixedID("entry")) }
func NewReportID() ReportID             { return ReportID(newPrefixedID("report")) }
func NewOperationID() OperationID       { return OperationID(newPrefixedID("op")) }

// TaskInputEntryID 根据主任务身份生成稳定的输入条目 ID。
func TaskInputEntryID(taskID TaskID) EntryID {
	return EntryID("input:" + taskID.String())
}

// Valid 判断项目身份是否满足统一身份契约，不执行规范化。
func (id ProjectID) Valid() bool { return validResourceID(string(id)) }

// Valid 判断会话身份是否满足统一身份契约，不执行规范化。
func (id SessionID) Valid() bool { return validResourceID(string(id)) }

// Valid 判断代理身份是否满足统一身份契约，不执行规范化。
func (id AgentID) Valid() bool { return validResourceID(string(id)) }

// validResourceID 约束可直接作为目录或文件名的资源身份，防路径逃逸、跨平台保留名与大小写冲突。
func validResourceID(value string) bool {
	if len(value) == 0 || len(value) > 180 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	switch value {
	case "con", "prn", "aux", "nul":
		return false
	}
	if len(value) == 4 && (value[:3] == "com" || value[:3] == "lpt") && value[3] >= '1' && value[3] <= '9' {
		return false
	}
	return true
}
