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
	Name        string `json:"name"`
	RuleID      string `json:"ruleId"`
	Arguments   string `json:"arguments"`
	Action      string `json:"action"`
	RiskVerdict string `json:"riskVerdict"`
	RiskReason  string `json:"riskReason"`
}

type ToolControlRequest struct {
	SessionID   string `json:"sessionId"`
	AgentID     string `json:"agentId"`
	ExecutionID string `json:"executionId"`
	CallID      string `json:"callId"`
}
