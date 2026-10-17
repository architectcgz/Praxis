package dto

// SetProjectApprovalModeRequest 设置项目覆盖；mode=null 表示继承全局授权模式。
type SetProjectApprovalModeRequest struct {
	ProjectID string  `json:"projectId"`
	Mode      *string `json:"mode"`
}

// ProjectApprovalMode 同时返回配置来源和当前有效模式，由后端负责继承解析。
type ProjectApprovalMode struct {
	ProjectID     string  `json:"projectId"`
	GlobalMode    string  `json:"globalMode"`
	ProjectMode   *string `json:"projectMode"`
	EffectiveMode string  `json:"effectiveMode"`
}
