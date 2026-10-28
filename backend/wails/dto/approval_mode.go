package dto

// SetSessionApprovalModeRequest 设置会话覆盖；mode=null 表示继承全局授权模式。
type SetSessionApprovalModeRequest struct {
	SessionID string  `json:"sessionId" validate:"session_id"`
	Mode      *string `json:"mode" validate:"omitempty,oneof=ask auto yolo"`
}

// SessionApprovalMode 同时返回配置来源和当前有效模式，由后端负责继承解析。
type SessionApprovalMode struct {
	SessionID     string  `json:"sessionId"`
	GlobalMode    string  `json:"globalMode"`
	SessionMode   *string `json:"sessionMode"`
	EffectiveMode string  `json:"effectiveMode"`
}
