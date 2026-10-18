package contracts

// ToolApproval 是可供用户判断的审批快照，决定必须绑定参数摘要。
type ToolApproval struct {
	SessionID           string
	AgentID             string
	TaskID              string
	CallID              string
	Name                string
	ArgumentsDigest     string
	RuleID              string
	NormalizedArguments string
	Path                string
	Action              string
	RiskVerdict         string
	RiskReason          string
}
