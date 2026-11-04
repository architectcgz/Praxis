package dto

type ToolDecisionRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"id"`
	RunID     string `json:"runId" validate:"id"`
	CallID    string `json:"callId" validate:"id"`
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

type ToolCancelRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"id"`
	RunID     string `json:"runId" validate:"id"`
	CallID    string `json:"callId" validate:"id"`
}
