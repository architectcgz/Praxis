package dto

import "encoding/json"

type ToolDecisionRequest struct {
	SessionID string `json:"sessionId"`
	AgentID   string `json:"agentId"`
	TaskID    string `json:"taskId"`
	CallID    string `json:"callId"`
}
type ToolApproval struct {
	ToolDecisionRequest
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
