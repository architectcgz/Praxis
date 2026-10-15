package dto

type ToolDecisionRequest struct {
	SessionID       string `json:"sessionId"`
	AgentID         string `json:"agentId"`
	TaskID          string `json:"taskId"`
	CallID          string `json:"callId"`
	ArgumentsDigest string `json:"argumentsDigest"`
}
type ToolApproval struct {
	ToolDecisionRequest
	Name    string `json:"name"`
	RuleID  string `json:"ruleId"`
	Command string `json:"command"`
	Path    string `json:"path"`
}
