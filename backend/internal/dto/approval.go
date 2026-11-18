package dto

// SetSessionApprovalModeRequest 设置会话覆盖；mode=null 表示继承全局授权模式。
type SetSessionApprovalModeRequest struct {
	SessionID string  `json:"sessionId" validate:"session_id"`
	Mode      *string `json:"mode" validate:"omitempty,oneof=ask auto yolo"`
}

// SessionApprovalMode 返回已保存的会话覆盖和当前已加载的有效模式；二者可在 reload 前不同。
type SessionApprovalMode struct {
	SessionID     string  `json:"sessionId"`
	GlobalMode    string  `json:"globalMode"`
	SessionMode   *string `json:"sessionMode"`
	EffectiveMode string  `json:"effectiveMode"`
}

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
