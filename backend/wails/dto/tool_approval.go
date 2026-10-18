package dto

type ToolDecisionRequest struct {
	SessionID       string `json:"sessionId"`
	AgentID         string `json:"agentId"`
	ExecutionID     string `json:"executionId"`
	CallID          string `json:"callId"`
	ArgumentsDigest string `json:"argumentsDigest"`
}
type ToolApproval struct {
	ToolDecisionRequest
	Name                string `json:"name"`
	RuleID              string `json:"ruleId"`
	NormalizedArguments string `json:"normalizedArguments"`
	Path                string `json:"path"`
	Action              string `json:"action"`
	RiskVerdict         string `json:"riskVerdict"`
	RiskReason          string `json:"riskReason"`
}
